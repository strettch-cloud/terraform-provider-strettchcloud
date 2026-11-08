package compute_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// TestMain hands control to resource.TestMain so `go test -sweep=all` runs the
// sweeper registered below instead of the normal tests. compute is the only
// package with a sweeper (data sources create nothing to clean up), so it is the
// only one that needs this.
func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("strettchcloud_compute", &resource.Sweeper{
		Name: "strettchcloud_compute",
		F:    sweepComputes,
	})
}

// sweepComputes deletes every compute left behind by a crashed acceptance run,
// identified by the tf-acc-test host-name prefix or tag. The test team's key can
// only touch test resources, so this can never affect customer computes.
func sweepComputes(string) error {
	ctx := context.Background()
	c := acctest.NewClientFromEnv()

	all, err := c.ListAllComputes(ctx)
	if err != nil {
		return fmt.Errorf("listing computes for sweep: %w", err)
	}

	for _, comp := range all {
		if !acctest.IsTestResource(comp.HostName, comp.Tags) {
			continue
		}
		if comp.State == client.StateDestroyed || comp.State == client.StateDestroying {
			continue
		}
		if err := c.DeleteCompute(ctx, comp.ID); err != nil {
			return fmt.Errorf("deleting leftover compute %s: %w", comp.ID, err)
		}
	}
	return nil
}
