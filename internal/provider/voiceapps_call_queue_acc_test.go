package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccCallQueue_basic creates a call queue with an overflow forward to a phone
// number, updates it in place, imports it by id and destroys it. Run with TF_ACC=1
// and ARM_* credentials pointing at a disposable dev tenant.
func TestAccCallQueue_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-cq")
	const rn = "teams_call_queue.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccCallQueueConfig(name, "round_robin", 30),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "name", name),
					resource.TestCheckResourceAttr(rn, "routing_method", "round_robin"),
					resource.TestCheckResourceAttr(rn, "agent_alert_time", "30"),
					resource.TestCheckResourceAttr(rn, "presence_based_routing", "false"),
					resource.TestCheckResourceAttr(rn, "overflow_action", "forward"),
					resource.TestCheckResourceAttr(rn, "overflow_action_target", "tel:+18005550100"),
					resource.TestCheckResourceAttr(rn, "timeout_action", "disconnect"),
					resource.TestCheckResourceAttr(rn, "no_agent_action", "queue"),
					resource.TestCheckNoResourceAttr(rn, "users.#"),
				),
			},
			{ // in-place update
				Config: testAccCallQueueConfig(name, "serial", 45),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "routing_method", "serial"),
					resource.TestCheckResourceAttr(rn, "agent_alert_time", "45"),
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

// TestAccCallQueue_users adds one agent, removes it, then destroys the queue. The
// agent is the Entra user object id in TEAMS_ACC_AGENT_ID; the test is skipped
// when it is unset.
func TestAccCallQueue_users(t *testing.T) {
	agent := os.Getenv("TEAMS_ACC_AGENT_ID")
	if agent == "" {
		t.Skip("set TEAMS_ACC_AGENT_ID to an Entra user object id to run the agent test")
	}
	name := acctest.RandomWithPrefix("tf-acc-cq")
	const rn = "teams_call_queue.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create with one agent
				Config: testAccCallQueueUsersConfig(name, fmt.Sprintf("[%q]", agent)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "users.#", "1"),
					resource.TestCheckResourceAttr(rn, "users.0", agent),
				),
			},
			{ // import by id
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{ // remove the agent
				Config: testAccCallQueueUsersConfig(name, "[]"),
				Check:  resource.TestCheckResourceAttr(rn, "users.#", "0"),
			},
		},
	})
}

func testAccCallQueueUsersConfig(name, users string) string {
	return fmt.Sprintf(`
resource "teams_call_queue" "test" {
  name  = %[1]q
  users = %[2]s
}
`, name, users)
}

func testAccCallQueueConfig(name, routingMethod string, agentAlertTime int) string {
	return fmt.Sprintf(`
resource "teams_call_queue" "test" {
  name                   = %[1]q
  routing_method         = %[2]q
  agent_alert_time       = %[3]d
  overflow_action        = "forward"
  overflow_action_target = "tel:+18005550100"
}
`, name, routingMethod, agentAlertTime)
}
