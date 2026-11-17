package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccDomain returns the verified tenant domain in TEAMS_ACC_DOMAIN, skipping
// the test when it is unset.
func testAccDomain(t *testing.T) string {
	t.Helper()
	domain := os.Getenv("TEAMS_ACC_DOMAIN")
	if domain == "" {
		t.Skip("set TEAMS_ACC_DOMAIN to a verified tenant domain to run the resource account tests")
	}
	return domain
}

// TestAccResourceAccount_basic creates a call queue resource account, renames it,
// imports it by object id and destroys it (Microsoft Graph delete). No license or
// phone number is assigned. Run with TF_ACC=1, ARM_* credentials pointing at a
// disposable dev tenant and TEAMS_ACC_DOMAIN.
func TestAccResourceAccount_basic(t *testing.T) {
	domain := testAccDomain(t)
	name := acctest.RandomWithPrefix("tf-acc-ra")
	upn := name + "@" + domain
	const rn = "teams_resource_account.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccResourceAccountConfig(upn, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "user_principal_name", upn),
					resource.TestCheckResourceAttr(rn, "display_name", name),
					resource.TestCheckResourceAttr(rn, "type", "call_queue"),
					resource.TestCheckNoResourceAttr(rn, "phone_number"),
				),
			},
			{ // rename
				Config: testAccResourceAccountConfig(upn, name+"-renamed"),
				Check:  resource.TestCheckResourceAttr(rn, "display_name", name+"-renamed"),
			},
			{ // import by object id
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccResourceAccountConfig(upn, displayName string) string {
	return fmt.Sprintf(`
resource "teams_resource_account" "test" {
  user_principal_name = %[1]q
  display_name        = %[2]q
  type                = "call_queue"
}
`, upn, displayName)
}
