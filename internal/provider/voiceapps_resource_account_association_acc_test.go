package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccResourceAccountAssociation_basic creates a call queue resource account, a
// call queue without agents and the association between them, imports the
// association by resource account id and destroys everything. Run with TF_ACC=1,
// ARM_* credentials pointing at a disposable dev tenant and TEAMS_ACC_DOMAIN.
func TestAccResourceAccountAssociation_basic(t *testing.T) {
	domain := testAccDomain(t)
	raName := acctest.RandomWithPrefix("tf-acc-ra")
	cqName := acctest.RandomWithPrefix("tf-acc-cq")
	const rn = "teams_resource_account_association.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccResourceAccountAssociationConfig(raName+"@"+domain, raName, cqName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(rn, "id", "teams_resource_account.test", "id"),
					resource.TestCheckResourceAttrPair(rn, "resource_account_id", "teams_resource_account.test", "id"),
					resource.TestCheckResourceAttrPair(rn, "configuration_id", "teams_call_queue.test", "id"),
					resource.TestCheckResourceAttr(rn, "configuration_type", "call_queue"),
					resource.TestCheckResourceAttr(rn, "call_priority", "3"),
				),
			},
			{ // import by resource account id
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccResourceAccountAssociationConfig(upn, raName, cqName string) string {
	return fmt.Sprintf(`
resource "teams_resource_account" "test" {
  user_principal_name = %[1]q
  display_name        = %[2]q
  type                = "call_queue"
}

resource "teams_call_queue" "test" {
  name = %[3]q
}

resource "teams_resource_account_association" "test" {
  resource_account_id = teams_resource_account.test.id
  configuration_id    = teams_call_queue.test.id
  configuration_type  = "call_queue"
}
`, upn, raName, cqName)
}
