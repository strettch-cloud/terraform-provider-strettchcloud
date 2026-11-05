package region_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

func TestAccDataSourceRegion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_region" "test" { region = "KGL-1" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.strettchcloud_region.test", "region", "KGL-1"),
					resource.TestCheckResourceAttrSet("data.strettchcloud_region.test", "country_code"),
				),
			},
		},
	})
}

func TestAccDataSourceRegions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_regions" "all" {}`,
				Check:  resource.TestCheckResourceAttrSet("data.strettchcloud_regions.all", "regions.#"),
			},
		},
	})
}
