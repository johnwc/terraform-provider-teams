resource "teams_resource_account" "support" {
  user_principal_name = "ra-support@contoso.com"
  display_name        = "Support line"
  type                = "call_queue"
}

resource "teams_call_queue" "support" {
  name = "Support"
}

resource "teams_resource_account_association" "support" {
  resource_account_id = teams_resource_account.support.id
  configuration_id    = teams_call_queue.support.id
  configuration_type  = "call_queue"
  call_priority       = 3
}
