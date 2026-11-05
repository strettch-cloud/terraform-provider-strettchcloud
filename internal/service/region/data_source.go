package region

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

var (
	_ datasource.DataSource              = &regionDataSource{}
	_ datasource.DataSourceWithConfigure = &regionDataSource{}
)

// NewDataSource returns the singular strettchcloud_region data source.
func NewDataSource() datasource.DataSource {
	return &regionDataSource{}
}

type regionDataSource struct {
	client *client.Client
}

func (d *regionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_region"
}

func (d *regionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Finds a single region by its slug. An unknown slug is an error when read.",
		Attributes: map[string]schema.Attribute{
			"region": schema.StringAttribute{
				MarkdownDescription: "Region slug, e.g. `KGL-1`. Filter and output.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Region name, e.g. `Kigali-1`. Output only.",
				Computed:            true,
			},
			"country_code": schema.StringAttribute{
				MarkdownDescription: "ISO country code, e.g. `RW`. Output only.",
				Computed:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Region identifier. Output only.",
				Computed:            true,
			},
		},
	}
}

func (d *regionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *regionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg regionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.client.ListRegions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading regions failed", err.Error())
		return
	}

	slug := cfg.Region.ValueString()
	valid := make([]string, len(all))
	for i, r := range all {
		if r.Region == slug {
			resp.Diagnostics.Append(resp.State.Set(ctx, fromAPI(r))...)
			return
		}
		valid[i] = r.Region
	}
	sort.Strings(valid)
	resp.Diagnostics.AddError("No matching region",
		fmt.Sprintf("No region has the slug %q. Valid values: %s.", slug, strings.Join(valid, ", ")))
}

var (
	_ datasource.DataSource              = &regionsDataSource{}
	_ datasource.DataSourceWithConfigure = &regionsDataSource{}
)

// NewListDataSource returns the plural strettchcloud_regions data source.
func NewListDataSource() datasource.DataSource {
	return &regionsDataSource{}
}

type regionsDataSource struct {
	client *client.Client
}

type regionsModel struct {
	Regions []regionModel `tfsdk:"regions"`
}

func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}

func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns every region in the catalog.",
		Attributes: map[string]schema.Attribute{
			"regions": schema.ListNestedAttribute{
				MarkdownDescription: "All regions.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"region":       schema.StringAttribute{MarkdownDescription: "Region slug.", Computed: true},
						"name":         schema.StringAttribute{MarkdownDescription: "Region name.", Computed: true},
						"country_code": schema.StringAttribute{MarkdownDescription: "ISO country code.", Computed: true},
						"id":           schema.StringAttribute{MarkdownDescription: "Region identifier.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *regionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *regionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := d.client.ListRegions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading regions failed", err.Error())
		return
	}

	state := regionsModel{Regions: make([]regionModel, 0, len(all))}
	for _, r := range all {
		state.Regions = append(state.Regions, fromAPI(r))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
