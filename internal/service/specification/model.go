// Package specification implements the strettchcloud_specification (one) and
// strettchcloud_specifications (all) data sources over GET /computes/specifications.
// memory_mb and storage_gb are floats, matching the live API.
package specification

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// specificationModel is one specification as Terraform sees it. In the singular
// data source the filter fields double as outputs.
type specificationModel struct {
	ID            types.String  `tfsdk:"id"`
	Specification types.String  `tfsdk:"specification"`
	VCPUs         types.Int64   `tfsdk:"vcpus"`
	MemoryMB      types.Float64 `tfsdk:"memory_mb"`
	StorageGB     types.Float64 `tfsdk:"storage_gb"`
}

// fromAPI maps an API specification into the Terraform model.
func fromAPI(s client.Specification) specificationModel {
	return specificationModel{
		ID:            types.StringValue(s.ID),
		Specification: types.StringValue(s.Specification),
		VCPUs:         types.Int64Value(int64(s.VCPUs)),
		MemoryMB:      types.Float64Value(s.MemoryMB),
		StorageGB:     types.Float64Value(s.StorageGB),
	}
}
