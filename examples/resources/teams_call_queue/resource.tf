resource "teams_call_queue" "support" {
  name           = "Support"
  language_id    = "en-US"
  routing_method = "round_robin"

  users = [
    "6f1c2b3a-0000-4000-8000-000000000001",
    "6f1c2b3a-0000-4000-8000-000000000002",
  ]

  welcome_text_to_speech_prompt = "Thanks for calling Support. Please hold for the next available agent."

  overflow_threshold                              = 20
  overflow_action                                 = "shared_voicemail"
  overflow_action_target                          = "0c7d4e5f-0000-4000-8000-0000000000aa" # Microsoft 365 group id
  overflow_shared_voicemail_text_to_speech_prompt = "All agents are busy. Please leave a message and we will call you back."
  enable_overflow_shared_voicemail_transcription  = true

  timeout_threshold     = 600
  timeout_action        = "forward"
  timeout_action_target = "tel:+18005550100"
}
