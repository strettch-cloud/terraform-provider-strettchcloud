package image_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

func TestAccDataSourceImage(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_image" "test" { image = "UBUNTU-24.04" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.strettchcloud_image.test", "image", "UBUNTU-24.04"),
					resource.TestCheckResourceAttrSet("data.strettchcloud_image.test", "display_name"),
				),
			},
		},
	})
}

func TestAccDataSourceImages(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { scacctest.PreCheck(t) },
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "strettchcloud_images" "all" {}`,
				Check:  resource.TestCheckResourceAttrSet("data.strettchcloud_images.all", "images.#"),
			},
		},
	})
}
