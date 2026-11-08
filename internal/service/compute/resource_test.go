package compute_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// testPublicKey is a throwaway ed25519 public key used by the acceptance tests.
// It is a real, valid key (the API rejects malformed keys); the matching private
// key is discarded.
const testPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJv/aq4dyqdRA/+3f0fuvig47TDGLcgKTJ3AK34+Ugl5 tf-acc-test"

// TestAccCompute covers the core lifecycle that works today: create, a no-op
// re-plan (idempotency), stop, start, and import. It runs only when TF_ACC is
// set.
func TestAccCompute(t *testing.T) {
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)
	resourceName := "strettchcloud_compute.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				// Create.
				Config: testAccComputeConfig(name, "S-1C-1G", []string{"production", "web"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "host_name", name),
					resource.TestCheckResourceAttr(resourceName, "state", client.StateRunning),
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "with_public_ip", "false"),
					resource.TestCheckResourceAttr(resourceName, "tags.#", "2"),
				),
			},
			{
				// Re-apply the same config: plan must be empty (idempotency).
				Config:   testAccComputeConfig(name, "S-1C-1G", []string{"production", "web"}),
				PlanOnly: true,
			},
			{
				// Stop the compute in place.
				Config: testAccComputeConfigState(name, client.StateStopped),
				Check:  resource.TestCheckResourceAttr(resourceName, "state", client.StateStopped),
			},
			{
				// Start it again.
				Config: testAccComputeConfigState(name, client.StateRunning),
				Check:  resource.TestCheckResourceAttr(resourceName, "state", client.StateRunning),
			},
			{
				// Import by id; public_keys is not recoverable.
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_keys", "timeouts"},
			},
		},
	})
}

// TestAccComputeUpdateHostNameTags covers the in-place update of host_name and
// tags. It depends on the PATCH /computes/{id} update endpoint, which is not yet
// deployed to production — this test fails with a 404 until the backend ships it.
func TestAccComputeUpdateHostNameTags(t *testing.T) {
	t.Skip("Pending backend: PATCH /computes/{id} update endpoint is not yet deployed to production. The update path is covered by the mock unit test TestUnitLifecycle_CreateStoppedThenUpdateAndResize; remove this skip once the endpoint ships.")
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)
	updatedName := acctest.RandomWithPrefix(scacctest.TestNamePrefix)
	resourceName := "strettchcloud_compute.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccComputeConfig(name, "S-1C-1G", []string{"production", "web"}),
			},
			{
				// Change host_name and tags in place; the id must not change.
				Config: testAccComputeConfig(updatedName, "S-1C-1G", []string{"staging"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "host_name", updatedName),
					resource.TestCheckResourceAttr(resourceName, "tags.#", "1"),
				),
			},
		},
	})
}

// TestAccComputeResize verifies an in-place upsize: the compute is stopped,
// resized, and restarted, keeping its id and ending in its configured RUNNING
// state.
func TestAccComputeResize(t *testing.T) {
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)
	resourceName := "strettchcloud_compute.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccComputeConfig(name, "S-1C-1G", []string{"web"}),
				Check:  resource.TestCheckResourceAttr(resourceName, "specification", "S-1C-1G"),
			},
			{
				// Upsize; the compute stops during the resize then starts again.
				Config: testAccComputeConfig(name, "S-1C-2G", []string{"web"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "specification", "S-1C-2G"),
					resource.TestCheckResourceAttr(resourceName, "state", client.StateRunning),
				),
			},
		},
	})
}

func testAccComputeConfigState(hostName, state string) string {
	return fmt.Sprintf(`
resource "strettchcloud_compute" "test" {
  host_name     = %q
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = [%q]
  tags          = ["production", "web"]
  state         = %q
}
`, hostName, testPublicKey, state)
}

func testAccComputeConfig(hostName, specification string, tags []string) string {
	tagList := ""
	for i, tag := range tags {
		if i > 0 {
			tagList += ", "
		}
		tagList += fmt.Sprintf("%q", tag)
	}
	return fmt.Sprintf(`
resource "strettchcloud_compute" "test" {
  host_name     = %q
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = %q
  public_keys   = [%q]
  tags          = [%s]
}
`, hostName, specification, testPublicKey, tagList)
}

// testAccCheckComputeDestroy asserts every compute in state is gone.
func testAccCheckComputeDestroy(s *terraform.State) error {
	ctx := context.Background()
	c := scacctest.NewClientFromEnv()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "strettchcloud_compute" {
			continue
		}
		got, err := c.GetCompute(ctx, rs.Primary.ID)
		if err != nil {
			if client.IsNotFound(err) {
				continue
			}
			return err
		}
		if got.State != client.StateDestroyed && got.State != client.StateDestroying {
			return fmt.Errorf("compute %s still exists in state %s", rs.Primary.ID, got.State)
		}
	}
	return nil
}
