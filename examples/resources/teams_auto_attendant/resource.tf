resource "teams_auto_attendant" "main" {
  name         = "Main line"
  language_id  = "en-US"
  time_zone_id = "Central Standard Time"
  voice_id     = "female"

  default_call_flow {
    greeting    = "Thanks for calling Contoso."
    menu_prompt = "For sales, press 1. For support, press 2. To hear our address, press 3."
    menu_option = [
      { key = "1", action = "transfer", target_id = "6f1c2b3a-0000-4000-8000-000000000001", target_type = "user" },
      { key = "2", action = "transfer", target_id = "tel:+18005550100", target_type = "external_pstn" },
      { key = "3", action = "announcement", announcement = "We are at 1 Main Street." },
    ]
  }

  after_hours {
    schedule_id = teams_schedule.business_hours.id
    greeting    = "We are closed. Please call back during business hours."
    menu_option = [{ key = "automatic", action = "disconnect" }]
  }

  holiday {
    schedule_id = teams_schedule.holidays.id
    greeting    = "We are closed for the holiday."
    menu_option = [{ key = "automatic", action = "disconnect" }]
  }
}
