package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccPhoneNumberAssignment_basic assigns the calling plan number in
// TEAMS_ACC_PHONE_NUMBER to the licensed user or resource account object id in
// TEAMS_ACC_PHONE_TARGET, imports the assignment and unassigns it. The test is
// skipped unless both are set. Run with TF_ACC=1 and ARM_* credentials pointing
// at a disposable dev tenant.
func TestAccPhoneNumberAssignment_basic(t *testing.T) {
	number, target := os.Getenv("TEAMS_ACC_PHONE_NUMBER"), os.Getenv("TEAMS_ACC_PHONE_TARGET")
	if number == "" || target == "" {
		t.Skip("set TEAMS_ACC_PHONE_NUMBER (E.164 calling plan number) and TEAMS_ACC_PHONE_TARGET (licensed object id) to run the phone number test")
	}
	const rn = "teams_phone_number_assignment.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // assign
				Config: testAccPhoneNumberAssignmentConfig(target, number),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "id", target+"/"+number),
					resource.TestCheckResourceAttr(rn, "identity", target),
					resource.TestCheckResourceAttr(rn, "phone_number", number),
					resource.TestCheckResourceAttr(rn, "phone_number_type", "calling_plan"),
				),
			},
			{ // import by <identity>/<phone_number>
				ResourceName:      rn,
				ImportState:       true,
				ImportStateId:     target + "/" + number,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccPhoneNumberAssignmentConfig(identity, number string) string {
	return fmt.Sprintf(`
resource "teams_phone_number_assignment" "test" {
  identity          = %[1]q
  phone_number      = %[2]q
  phone_number_type = "calling_plan"
}
`, identity, number)
}
