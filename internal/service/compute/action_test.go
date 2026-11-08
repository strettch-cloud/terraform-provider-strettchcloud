package compute_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// TestAccActionComputeReboot triggers the reboot action from an after_create
// lifecycle event and verifies the compute is RUNNING afterwards. after_create
// is used (rather than after_update) so the test does not depend on the
// not-yet-shipped update endpoint. Needs Terraform 1.14+.
func TestAccActionComputeReboot(t *testing.T) {
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)
	resourceName := "strettchcloud_compute.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		CheckDestroy: testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRebootConfig(name),
				Check:  resource.TestCheckResourceAttr(resourceName, "state", client.StateRunning),
			},
		},
	})
}

func testAccRebootConfig(hostName string) string {
	return fmt.Sprintf(`
resource "strettchcloud_compute" "test" {
  host_name     = %q
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = [%[2]q]
  tags          = ["web"]

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.strettchcloud_compute_reboot.test]
    }
  }
}

action "strettchcloud_compute_reboot" "test" {
  config {
    compute_id = strettchcloud_compute.test.id
  }
}
`, hostName, testPublicKey)
}
