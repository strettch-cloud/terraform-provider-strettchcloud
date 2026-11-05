package specification_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

func TestAccDataSourceSpecification(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_specification" "test" { specification = "S-1C-1G" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.strettchcloud_specification.test", "specification", "S-1C-1G"),
					resource.TestCheckResourceAttrSet("data.strettchcloud_specification.test", "vcpus"),
				),
			},
		},
	})
}

func TestAccDataSourceSpecifications(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_specifications" "all" {}`,
				Check: resource.TestCheckResourceAttrSet(
					"data.strettchcloud_specifications.all", "specifications.#",
				),
			},
		},
	})
}
