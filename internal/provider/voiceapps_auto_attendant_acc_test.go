package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccAutoAttendant_basic creates a weekly and a holiday schedule and an auto
// attendant whose menu transfers to two phone numbers, updates the menu in place,
// imports it by id and destroys everything. No users, resource accounts or phone
// numbers are assigned. Run with TF_ACC=1 and ARM_* credentials pointing at a
// disposable dev tenant.
func TestAccAutoAttendant_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-aa")
	const rn = "teams_auto_attendant.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccAutoAttendantConfig(name, "Press 1 for sales. Press 2 for support.", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "name", name),
					resource.TestCheckResourceAttr(rn, "language_id", "en-US"),
					resource.TestCheckResourceAttr(rn, "voice_id", "female"),
					resource.TestCheckResourceAttr(rn, "time_zone_id", "Central Standard Time"),
					resource.TestCheckResourceAttrSet(rn, "default_call_flow.id"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.#", "2"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.key", "1"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.action", "transfer"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.target_id", "tel:+18005550101"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.0.target_type", "external_pstn"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.1.target_id", "tel:+18005550102"),
					resource.TestCheckResourceAttrSet(rn, "after_hours.id"),
					resource.TestCheckResourceAttrPair(rn, "after_hours.schedule_id", "teams_schedule.hours", "id"),
					resource.TestCheckResourceAttr(rn, "after_hours.menu_option.0.action", "disconnect"),
					resource.TestCheckResourceAttr(rn, "holiday.#", "1"),
					resource.TestCheckResourceAttrSet(rn, "holiday.0.id"),
					resource.TestCheckResourceAttrPair(rn, "holiday.0.schedule_id", "teams_schedule.holiday", "id"),
					resource.TestCheckResourceAttr(rn, "holiday.0.menu_option.0.action", "disconnect"),
				),
			},
			{ // in-place update
				Config: testAccAutoAttendantConfig(name, "Press 1 for sales, 2 for support or 3 for billing.",
					`{ key = "3", action = "transfer", target_id = teams_call_queue.billing.id, target_type = "voice_app" },
    { key = "4", action = "announcement", announcement = "Our hours are 8 to 5." },`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_prompt", "Press 1 for sales, 2 for support or 3 for billing."),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.#", "4"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.3.action", "announcement"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.3.announcement", "Our hours are 8 to 5."),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.2.key", "3"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.2.action", "transfer"),
					resource.TestCheckResourceAttr(rn, "default_call_flow.menu_option.2.target_type", "voice_app"),
					resource.TestCheckResourceAttrPair(rn, "default_call_flow.menu_option.2.target_id", "teams_call_queue.billing", "id"),
				),
			},
			{ // import by id
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccAutoAttendantConfig(name, menuPrompt, extraOption string) string {
	return fmt.Sprintf(`
resource "teams_call_queue" "billing" {
  name = "%[1]s-billing"
}

resource "teams_schedule" "hours" {
  name = "%[1]s-hours"
  weekly {
    monday    = [{ start = "08:00", end = "17:00" }]
    tuesday   = [{ start = "08:00", end = "17:00" }]
    wednesday = [{ start = "08:00", end = "17:00" }]
    thursday  = [{ start = "08:00", end = "17:00" }]
    friday    = [{ start = "08:00", end = "17:00" }]
  }
}

resource "teams_schedule" "holiday" {
  name = "%[1]s-holiday"
  fixed {
    date_range = [{ start = "2030-12-25T00:00:00", end = "2030-12-26T00:00:00" }]
  }
}

resource "teams_auto_attendant" "test" {
  name         = %[1]q
  time_zone_id = "Central Standard Time"

  default_call_flow {
    greeting    = "Thanks for calling."
    menu_prompt = %[2]q
    menu_option = [
      { key = "1", action = "transfer", target_id = "tel:+18005550101", target_type = "external_pstn" },
      { key = "2", action = "transfer", target_id = "tel:+18005550102", target_type = "external_pstn" },
      %[3]s
    ]
  }

  after_hours {
    schedule_id = teams_schedule.hours.id
    greeting    = "We are closed."
    menu_option = [{ key = "automatic", action = "disconnect" }]
  }

  holiday {
    schedule_id = teams_schedule.holiday.id
    greeting    = "We are closed for the holiday."
    menu_option = [{ key = "automatic", action = "disconnect" }]
  }
}
`, name, menuPrompt, extraOption)
}
