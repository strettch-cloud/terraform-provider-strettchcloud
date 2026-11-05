// Package region implements the strettchcloud_region (one) and
// strettchcloud_regions (all) data sources over GET /regions.
package region

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// regionModel is one region as Terraform sees it. id is the numeric region id
// rendered as a string.
type regionModel struct {
	Region      types.String `tfsdk:"region"`
	Name        types.String `tfsdk:"name"`
	CountryCode types.String `tfsdk:"country_code"`
	ID          types.String `tfsdk:"id"`
}

func fromAPI(r client.Region) regionModel {
	return regionModel{
		Region:      types.StringValue(r.Region),
		Name:        types.StringValue(r.Name),
		CountryCode: types.StringValue(r.CountryCode),
		ID:          types.StringValue(strconv.Itoa(r.ID)),
	}
}
