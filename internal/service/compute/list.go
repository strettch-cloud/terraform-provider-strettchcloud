package compute

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

// List resource (Terraform Search, v1.1.0). It enumerates every compute in the
// team and returns each one's identity, so `terraform query` can discover and
// bulk-import computes created outside Terraform. There is no config and no
// filtering; computes are never looked up by tag.
var (
	_ list.ListResource              = &computeList{}
	_ list.ListResourceWithConfigure = &computeList{}
)

// NewListResource returns the strettchcloud_compute list resource. Its name must
// match the managed resource.
func NewListResource() list.ListResource {
	return &computeList{}
}

type computeList struct {
	client *client.Client
}

func (l *computeList) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute"
}

func (l *computeList) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every compute instance in the team, for discovery and bulk import with `terraform query`.",
	}
}

func (l *computeList) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	l.client = svcutil.ResourceClient(req, resp)
}

// listResourceModel builds the attributes Terraform uses for -generate-config-out.
// It adds tags and with_public_ip, which applyComputed does not set, to prevent
// lost tags and unnecessary replacements when the generated config is applied.
func listResourceModel(ctx context.Context, c *client.Compute) (computeResourceModel, diag.Diagnostics) {
	var m computeResourceModel
	var diags diag.Diagnostics
	m.applyComputed(c)
	diags.Append(m.applyTags(ctx, c.Tags, true)...)
	m.WithPublicIP = types.BoolValue(c.IPv4 != "")
	// public_keys and timeouts are not API values; set typed nulls so the
	// generated config (terraform query -generate-config-out) marshals cleanly
	// instead of failing on an untyped null. The timeouts attributes mirror the
	// Create/Update/Delete opts declared in the resource schema.
	m.PublicKeys = types.SetNull(types.StringType)
	m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{
		"create": types.StringType,
		"update": types.StringType,
		"delete": types.StringType,
	})}
	return m, diags
}

func (l *computeList) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	computes, err := l.client.ListAllComputes(ctx)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Listing computes failed", err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, c := range computes {
			result := req.NewListResult(ctx)
			result.DisplayName = c.HostName

			result.Diagnostics.Append(result.Identity.Set(ctx, computeIdentityModel{ID: types.StringValue(c.ID)})...)

			// Populate the resource attributes when Terraform asks for them
			// (terraform query -generate-config-out). Only stable state is
			// reported.
			if req.IncludeResource {
				model, diags := listResourceModel(ctx, &c)
				result.Diagnostics.Append(diags...)
				result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
			}

			if !push(result) {
				return
			}
		}
	}
}
