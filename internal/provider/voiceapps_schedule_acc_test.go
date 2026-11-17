package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSchedule_weekly creates a weekly schedule, updates its hours, imports it
// by id and destroys it. Run with TF_ACC=1 and ARM_* credentials pointing at a
// disposable dev tenant.
func TestAccSchedule_weekly(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-sched")
	const rn = "teams_schedule.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccScheduleWeeklyConfig(name, "08:00", "17:00"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "name", name),
					resource.TestCheckResourceAttr(rn, "weekly.monday.#", "1"),
					resource.TestCheckResourceAttr(rn, "weekly.monday.0.start", "08:00"),
					resource.TestCheckResourceAttr(rn, "weekly.monday.0.end", "17:00"),
					resource.TestCheckResourceAttr(rn, "weekly.friday.0.start", "08:00"),
					resource.TestCheckNoResourceAttr(rn, "weekly.saturday.#"),
					resource.TestCheckNoResourceAttr(rn, "fixed.date_range.#"),
				),
			},
			{ // in-place update
				Config: testAccScheduleWeeklyConfig(name, "09:00:00", "17:30:00"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "weekly.monday.0.start", "09:00:00"),
					resource.TestCheckResourceAttr(rn, "weekly.monday.0.end", "17:30:00"),
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

// TestAccSchedule_fixed creates a fixed (holiday) schedule and destroys it.
func TestAccSchedule_fixed(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-sched")
	const rn = "teams_schedule.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccScheduleFixedConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "name", name),
					resource.TestCheckResourceAttr(rn, "fixed.date_range.#", "1"),
					resource.TestCheckResourceAttr(rn, "fixed.date_range.0.start", "2030-12-25T00:00:00"),
					resource.TestCheckResourceAttr(rn, "fixed.date_range.0.end", "2030-12-26T00:00:00"),
				),
			},
		},
	})
}

func testAccScheduleWeeklyConfig(name, start, end string) string {
	return fmt.Sprintf(`
resource "teams_schedule" "test" {
  name = %[1]q
  weekly {
    monday    = [{ start = %[2]q, end = %[3]q }]
    tuesday   = [{ start = %[2]q, end = %[3]q }]
    wednesday = [{ start = %[2]q, end = %[3]q }]
    thursday  = [{ start = %[2]q, end = %[3]q }]
    friday    = [{ start = %[2]q, end = %[3]q }]
  }
}
`, name, start, end)
}

func testAccScheduleFixedConfig(name string) string {
	return fmt.Sprintf(`
resource "teams_schedule" "test" {
  name = %[1]q
  fixed {
    date_range = [{ start = "2030-12-25T00:00:00", end = "2030-12-26T00:00:00" }]
  }
}
`, name)
}
