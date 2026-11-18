resource "teams_audio_file" "greeting" {
  usage  = "auto_attendant"
  source = "${path.module}/audio/greeting.wav"
}

resource "teams_audio_file" "hold_music" {
  usage     = "call_queue"
  source    = "${path.module}/audio/hold-music.mp3"
  file_name = "support-hold-music.mp3"
}

# Link each file in the same apply; Teams deletes unused files within about 48 hours.
resource "teams_auto_attendant" "main" {
  name         = "Main line"
  time_zone_id = "Central Standard Time"

  default_call_flow {
    greeting_audio_file_id = teams_audio_file.greeting.id
    menu_prompt            = "For support, press 1."
    menu_option = [
      { key = "1", action = "transfer", target_id = "tel:+18005550100", target_type = "external_pstn" },
    ]
  }
}

resource "teams_call_queue" "support" {
  name                        = "Support"
  music_on_hold_audio_file_id = teams_audio_file.hold_music.id
}
