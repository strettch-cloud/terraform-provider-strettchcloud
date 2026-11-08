// Package compute implements the strettchcloud_compute resource and the
// strettchcloud_compute data source. This file holds the data source, which
// looks up one compute by id.
package compute

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

var (
	_ datasource.DataSource              = &computeDataSource{}
	_ datasource.DataSourceWithConfigure = &computeDataSource{}
)

// NewDataSource returns the strettchcloud_compute (by id) data source.
func NewDataSource() datasource.DataSource {
	return &computeDataSource{}
}

type computeDataSource struct {
	client *client.Client
}

// computeDataSourceModel is one compute as the data source reports it. id is the
// required filter; every other field is a computed output.
type computeDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	HostName      types.String `tfsdk:"host_name"`
	State         types.String `tfsdk:"state"`
	Image         types.String `tfsdk:"image"`
	Region        types.String `tfsdk:"region"`
	Specification types.String `tfsdk:"specification"`
	IPv4          types.String `tfsdk:"ipv4"`
	PrivateIPv4   types.String `tfsdk:"private_ipv4"`
	Tags          types.List   `tfsdk:"tags"`
	WithPublicIP  types.Bool   `tfsdk:"with_public_ip"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func (d *computeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute"
}

func (d *computeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single compute instance by its id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Compute id to look up.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"host_name":      schema.StringAttribute{MarkdownDescription: "Host name.", Computed: true},
			"state":          schema.StringAttribute{MarkdownDescription: "Power state.", Computed: true},
			"image":          schema.StringAttribute{MarkdownDescription: "Image slug.", Computed: true},
			"region":         schema.StringAttribute{MarkdownDescription: "Region slug.", Computed: true},
			"specification":  schema.StringAttribute{MarkdownDescription: "Specification slug.", Computed: true},
			"ipv4":           schema.StringAttribute{MarkdownDescription: "Public IPv4 address.", Computed: true},
			"private_ipv4":   schema.StringAttribute{MarkdownDescription: "Private IPv4 address.", Computed: true},
			"with_public_ip": schema.BoolAttribute{MarkdownDescription: "Whether the compute has a public IP.", Computed: true},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"created_at": schema.StringAttribute{MarkdownDescription: "Creation timestamp.", Computed: true},
			"updated_at": schema.StringAttribute{MarkdownDescription: "Last update timestamp.", Computed: true},
		},
	}
}

func (d *computeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *computeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg computeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := d.client.GetCompute(ctx, cfg.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("No matching compute", "No compute with id "+cfg.ID.ValueString()+".")
			return
		}
		resp.Diagnostics.AddError("Reading compute failed", err.Error())
		return
	}

	tags, diags := types.ListValueFrom(ctx, types.StringType, c.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	withPublicIP := c.IPv4 != ""
	if c.WithPublicIP != nil {
		withPublicIP = *c.WithPublicIP
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, computeDataSourceModel{
		ID:            types.StringValue(c.ID),
		HostName:      types.StringValue(c.HostName),
		State:         types.StringValue(c.State),
		Image:         types.StringValue(c.Image),
		Region:        types.StringValue(c.Region),
		Specification: types.StringValue(c.Specification),
		IPv4:          types.StringValue(c.IPv4),
		PrivateIPv4:   types.StringValue(c.PrivateIPv4),
		Tags:          tags,
		WithPublicIP:  types.BoolValue(withPublicIP),
		CreatedAt:     types.StringValue(c.CreatedAt),
		UpdatedAt:     types.StringValue(c.UpdatedAt),
	})...)
}
