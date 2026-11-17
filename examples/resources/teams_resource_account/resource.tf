resource "teams_resource_account" "support" {
  user_principal_name = "ra-support@contoso.com"
  display_name        = "Support line"
  type                = "call_queue"
}
