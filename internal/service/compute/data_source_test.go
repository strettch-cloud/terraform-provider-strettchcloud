package compute_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

// TestAccDataSourceCompute creates a compute and reads it back by id.
func TestAccDataSourceCompute(t *testing.T) {
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccComputeConfig(name, "S-1C-1G", []string{"web"}) + `
data "strettchcloud_compute" "by_id" {
  id = strettchcloud_compute.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.strettchcloud_compute.by_id", "host_name",
						"strettchcloud_compute.test", "host_name",
					),
					resource.TestCheckResourceAttrPair(
						"data.strettchcloud_compute.by_id", "state",
						"strettchcloud_compute.test", "state",
					),
				),
			},
		},
	})
}
