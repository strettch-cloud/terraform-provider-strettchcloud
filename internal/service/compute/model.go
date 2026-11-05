package compute

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// computeResourceModel is the strettchcloud_compute resource state. One schema
// serves both directions: inputs are what the customer writes and what POST
// /computes receives; outputs are filled from the API response.
type computeResourceModel struct {
	ID            types.String `tfsdk:"id"`
	HostName      types.String `tfsdk:"host_name"`
	Image         types.String `tfsdk:"image"`
	Region        types.String `tfsdk:"region"`
	Specification types.String `tfsdk:"specification"`
	PublicKeys    types.Set    `tfsdk:"public_keys"`
	WithPublicIP  types.Bool   `tfsdk:"with_public_ip"`
	Tags          types.List   `tfsdk:"tags"`
	State         types.String `tfsdk:"state"`

	IPv4        types.String `tfsdk:"ipv4"`
	PrivateIPv4 types.String `tfsdk:"private_ipv4"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// toCreateRequest builds the POST /computes body from the plan. state is never
// sent; every new compute starts.
func (m computeResourceModel) toCreateRequest(ctx context.Context) (client.ComputeCreateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	publicKeys, d := stringSlice(ctx, m.PublicKeys)
	diags.Append(d...)

	tags, d := stringSlice(ctx, m.Tags)
	diags.Append(d...)

	return client.ComputeCreateRequest{
		HostName:      m.HostName.ValueString(),
		Image:         m.Image.ValueString(),
		Region:        m.Region.ValueString(),
		Specification: m.Specification.ValueString(),
		Tags:          tags,
		WithPublicIP:  m.WithPublicIP.ValueBool(),
		PublicKeys:    publicKeys,
	}, diags
}

// applyComputed fills the fields the API always returns (id, host_name, image,
// region, specification, ipv4, private_ipv4, created_at, updated_at) plus the
// stable state. It leaves public_keys/with_public_ip (not returned by the API)
// and the tags normalisation to the caller, which applies the drift rules.
func (m *computeResourceModel) applyComputed(c *client.Compute) {
	m.ID = types.StringValue(c.ID)
	m.HostName = types.StringValue(c.HostName)
	m.Image = types.StringValue(c.Image)
	m.Region = types.StringValue(c.Region)
	m.Specification = types.StringValue(c.Specification)
	m.IPv4 = types.StringValue(c.IPv4)
	m.PrivateIPv4 = types.StringValue(c.PrivateIPv4)
	m.CreatedAt = types.StringValue(c.CreatedAt)
	m.UpdatedAt = types.StringValue(c.UpdatedAt)

	// Only stable states ever reach state. A transitional value keeps the prior
	// stable value already in m.State.
	if client.IsStableState(c.State) {
		m.State = types.StringValue(c.State)
	}
}

// applyTags maps the API's tags into state following the null/empty rule: the
// API returns [] for a compute with no tags, so when the config left tags null
// we keep it null to avoid a perpetual diff; an explicit [] stays [].
// configTagsNull reports whether the prior config/plan left tags null.
func (m *computeResourceModel) applyTags(ctx context.Context, apiTags []string, configTagsNull bool) diag.Diagnostics {
	if len(apiTags) == 0 && configTagsNull {
		m.Tags = types.ListNull(types.StringType)
		return nil
	}
	if apiTags == nil {
		apiTags = []string{}
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, apiTags)
	m.Tags = list
	return diags
}

// stringSlice extracts a []string from a types.List or types.Set, returning nil
// (not an empty non-nil slice) when the collection is null or unknown.
func stringSlice(ctx context.Context, v attrCollection) ([]string, diag.Diagnostics) {
	if v.IsNull() || v.IsUnknown() {
		return nil, nil
	}
	var out []string
	diags := v.ElementsAs(ctx, &out, false)
	return out, diags
}

// attrCollection is the shared surface of types.List and types.Set that
// stringSlice needs.
type attrCollection interface {
	IsNull() bool
	IsUnknown() bool
	ElementsAs(ctx context.Context, target any, allowUnhandled bool) diag.Diagnostics
}
