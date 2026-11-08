package compute_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

// TestAccListCompute exercises the list resource via `terraform query`. A compute
// is created first (which also establishes the provider), then a query step
// enumerates the team's computes. Needs Terraform 1.14+.
func TestAccListCompute(t *testing.T) {
	name := acctest.RandomWithPrefix(scacctest.TestNamePrefix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		CheckDestroy: testAccCheckComputeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccComputeConfig(name, "S-1C-1G", []string{"web"}),
			},
			{
				Query: true,
				Config: `
provider "strettchcloud" {}

list "strettchcloud_compute" "all" {
  provider = strettchcloud
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("strettchcloud_compute.all", 1),
				},
			},
		},
	})
}
