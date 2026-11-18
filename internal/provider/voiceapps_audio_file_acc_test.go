package provider

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccWriteWAV writes a 2-second 16 kHz mono 16-bit PCM sine tone to a temp
// directory and returns its path.
func testAccWriteWAV(t *testing.T, name string, freq float64) string {
	t.Helper()
	const rate, seconds = 16000, 2
	n := rate * seconds
	var buf bytes.Buffer
	w := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	buf.WriteString("RIFF")
	w(uint32(36 + n*2))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	w(uint32(16))       // fmt chunk size
	w(uint16(1))        // PCM
	w(uint16(1))        // mono
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // byte rate
	w(uint16(2))        // block align
	w(uint16(16))       // bits per sample
	buf.WriteString("data")
	w(uint32(n * 2))
	for i := 0; i < n; i++ {
		w(int16(0.3 * math.MaxInt16 * math.Sin(2*math.Pi*freq*float64(i)/rate)))
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAccAudioFile_autoAttendant uploads a WAV file and links it to an auto
// attendant greeting and a key 2 announcement, imports the auto attendant by id
// and destroys everything. Run with TF_ACC=1 and ARM_* credentials pointing at
// a disposable dev tenant.
func TestAccAudioFile_autoAttendant(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-aa")
	src := testAccWriteWAV(t, "greeting.wav", 440)
	const rn, fileRn = "teams_auto_attendant.test", "teams_audio_file.greeting"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccAudioFileAutoAttendantConfig(name, src),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(fileRn, "id"),
					resource.TestCheckResourceAttr(fileRn, "usage", "auto_attendant"),
					resource.TestCheckResourceAttr(fileRn, "file_name", "greeting.wav"),
					resource.TestCheckResourceAttrSet(fileRn, "source_sha256"),
					resource.TestCheckResourceAttrSet(fileRn, "duration"),
					resource.TestCheckResourceAttrPair(rn, "default_call_flow.greeting_audio_file_id", fileRn, "id"),
					resource.TestCheckNoResourceAttr(rn, "default_call_flow.greeting"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.#", "1"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.key", "2"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.action", "announcement"),
					resource.TestCheckResourceAttrPair(rn, "default_call_flow.menu_option.0.announcement_audio_file_id", fileRn, "id"),
					resource.TestCheckNoResourceAttr(rn, "default_call_flow.menu_option.0.announcement"),
				),
			},
			{ // refresh: linking the file clears the scheduled deletion
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr(fileRn, "scheduled_for_deletion", "false"),
			},
			{ // import the auto attendant by id
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccAudioFile_callQueue uploads a WAV file and uses it as the overflow
// shared voicemail greeting and the music on hold of a call queue, then destroys
// everything. The shared voicemail target is the Microsoft 365 group id in
// TEAMS_ACC_GROUP_ID; the test is skipped when it is unset.
func TestAccAudioFile_callQueue(t *testing.T) {
	group := os.Getenv("TEAMS_ACC_GROUP_ID")
	if group == "" {
		t.Skip("set TEAMS_ACC_GROUP_ID to a Microsoft 365 group id to run the call queue audio file test")
	}
	name := acctest.RandomWithPrefix("tf-acc-cq")
	src := testAccWriteWAV(t, "queue.wav", 660)
	const rn, fileRn = "teams_call_queue.test", "teams_audio_file.queue"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccAudioFileCallQueueConfig(name, src, group),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(fileRn, "id"),
					resource.TestCheckResourceAttr(fileRn, "usage", "call_queue"),
					resource.TestCheckResourceAttr(fileRn, "file_name", "queue.wav"),
					resource.TestCheckResourceAttr(rn, "overflow_action", "shared_voicemail"),
					resource.TestCheckResourceAttr(rn, "overflow_action_target", group),
					resource.TestCheckResourceAttrPair(rn, "overflow_shared_voicemail_audio_file_id", fileRn, "id"),
					resource.TestCheckNoResourceAttr(rn, "overflow_shared_voicemail_text_to_speech_prompt"),
					resource.TestCheckResourceAttrPair(rn, "music_on_hold_audio_file_id", fileRn, "id"),
				),
			},
		},
	})
}

func testAccAudioFileAutoAttendantConfig(name, src string) string {
	return fmt.Sprintf(`
resource "teams_audio_file" "greeting" {
  usage  = "auto_attendant"
  source = %[2]q
}

resource "teams_auto_attendant" "test" {
  name         = %[1]q
  time_zone_id = "Central Standard Time"

  default_call_flow {
    greeting_audio_file_id = teams_audio_file.greeting.id
    menu_prompt            = "Press 2 to hear our hours."
    menu_option = [
      { key = "2", action = "announcement", announcement_audio_file_id = teams_audio_file.greeting.id },
    ]
  }
}
`, name, src)
}

func testAccAudioFileCallQueueConfig(name, src, group string) string {
	return fmt.Sprintf(`
resource "teams_audio_file" "queue" {
  usage  = "call_queue"
  source = %[2]q
}

resource "teams_call_queue" "test" {
  name                                    = %[1]q
  overflow_action                         = "shared_voicemail"
  overflow_action_target                  = %[3]q
  overflow_shared_voicemail_audio_file_id = teams_audio_file.queue.id
  music_on_hold_audio_file_id             = teams_audio_file.queue.id
}
`, name, src, group)
}
