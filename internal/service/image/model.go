// Package image implements the strettchcloud_image (one) and strettchcloud_images
// (all) data sources over GET /images.
package image

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// imageModel is one image as Terraform sees it. In the singular data source the
// filter fields double as outputs.
type imageModel struct {
	Image       types.String `tfsdk:"image"`
	ImageName   types.String `tfsdk:"image_name"`
	Version     types.String `tfsdk:"version"`
	DisplayName types.String `tfsdk:"display_name"`
}

func fromAPI(i client.Image) imageModel {
	return imageModel{
		Image:       types.StringValue(i.Image),
		ImageName:   types.StringValue(i.ImageName),
		Version:     types.StringValue(i.Version),
		DisplayName: types.StringValue(i.DisplayName),
	}
}
