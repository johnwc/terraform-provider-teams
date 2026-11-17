resource "teams_resource_account" "support" {
  user_principal_name = "support-queue@example.onmicrosoft.com"
  display_name        = "Support queue"
  type                = "call_queue"
}

# The resource account needs a Teams Phone Resource Account license, plus a
# calling plan or Communications Credits for calling_plan numbers.
resource "teams_phone_number_assignment" "support" {
  identity          = teams_resource_account.support.id
  phone_number      = "+18005550100"
  phone_number_type = "calling_plan"
}
