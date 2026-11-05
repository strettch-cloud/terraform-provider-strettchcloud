package specification

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

// Interface assertions.
var (
	_ datasource.DataSource                     = &specificationDataSource{}
	_ datasource.DataSourceWithConfigure        = &specificationDataSource{}
	_ datasource.DataSourceWithConfigValidators = &specificationDataSource{}
)

// NewDataSource returns the singular strettchcloud_specification data source.
func NewDataSource() datasource.DataSource {
	return &specificationDataSource{}
}

type specificationDataSource struct {
	client *client.Client
}

func (d *specificationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_specification"
}

func (d *specificationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Finds a single compute specification by filter. At least one filter is required; filters combine with AND. Zero matches or more than one match is an error when read.",
		Attributes: map[string]schema.Attribute{
			"specification": schema.StringAttribute{
				MarkdownDescription: "Specification slug, e.g. `S-1C-1G`. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"vcpus": schema.Int64Attribute{
				MarkdownDescription: "Number of vCPUs. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"memory_mb": schema.Float64Attribute{
				MarkdownDescription: "Memory in MB. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Float64{float64validator.AtLeast(1)},
			},
			"storage_gb": schema.Float64Attribute{
				MarkdownDescription: "Storage in GB. Output only.",
				Computed:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Specification identifier. Output only.",
				Computed:            true,
			},
		},
	}
}

func (d *specificationDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	// At least one filter is required.
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(
			path.MatchRoot("specification"),
			path.MatchRoot("vcpus"),
			path.MatchRoot("memory_mb"),
		),
	}
}

func (d *specificationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *specificationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg specificationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.client.ListAllSpecifications(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading specifications failed", err.Error())
		return
	}

	matches := matchSpecifications(all, cfg)

	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No matching specification",
			fmt.Sprintf("No specification matches the given filters (%s). Valid values: %s.", describeFilters(cfg), specSlugs(all)))
		return
	case 1:
		resp.Diagnostics.Append(resp.State.Set(ctx, fromAPI(matches[0]))...)
	default:
		resp.Diagnostics.AddError("Ambiguous specification", fmt.Sprintf("%d specifications match the given filters; add more filters to select exactly one.", len(matches)))
	}
}

// specSlugs returns the catalog's specification slugs, sorted and comma-joined,
// for a readable "valid values: ..." suggestion in the no-match error.
func specSlugs(all []client.Specification) string {
	slugs := make([]string, len(all))
	for i, s := range all {
		slugs[i] = s.Specification
	}
	sort.Strings(slugs)
	return strings.Join(slugs, ", ")
}

// matchSpecifications returns the specifications satisfying every provided
// filter (AND). It is a pure function so the 0/1/many behaviour can be
// unit-tested.
func matchSpecifications(all []client.Specification, cfg specificationModel) []client.Specification {
	matches := make([]client.Specification, 0, len(all))
	for _, s := range all {
		if !cfg.Specification.IsNull() && s.Specification != cfg.Specification.ValueString() {
			continue
		}
		if !cfg.VCPUs.IsNull() && int64(s.VCPUs) != cfg.VCPUs.ValueInt64() {
			continue
		}
		if !cfg.MemoryMB.IsNull() && s.MemoryMB != cfg.MemoryMB.ValueFloat64() {
			continue
		}
		matches = append(matches, s)
	}
	return matches
}

func describeFilters(cfg specificationModel) string {
	var parts []string
	if !cfg.Specification.IsNull() {
		parts = append(parts, "specification="+cfg.Specification.ValueString())
	}
	if !cfg.VCPUs.IsNull() {
		parts = append(parts, fmt.Sprintf("vcpus=%d", cfg.VCPUs.ValueInt64()))
	}
	if !cfg.MemoryMB.IsNull() {
		parts = append(parts, fmt.Sprintf("memory_mb=%g", cfg.MemoryMB.ValueFloat64()))
	}
	return strings.Join(parts, ", ")
}

// Interface assertions.
var (
	_ datasource.DataSource              = &specificationsDataSource{}
	_ datasource.DataSourceWithConfigure = &specificationsDataSource{}
)

// NewListDataSource returns the plural strettchcloud_specifications data source.
func NewListDataSource() datasource.DataSource {
	return &specificationsDataSource{}
}

type specificationsDataSource struct {
	client *client.Client
}

// specificationsModel is the plural data source state: one computed list of all
// specifications.
type specificationsModel struct {
	Specifications []specificationModel `tfsdk:"specifications"`
}

func (d *specificationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_specifications"
}

func (d *specificationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns every compute specification in the catalog.",
		Attributes: map[string]schema.Attribute{
			"specifications": schema.ListNestedAttribute{
				MarkdownDescription: "All compute specifications.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"specification": schema.StringAttribute{MarkdownDescription: "Specification slug.", Computed: true},
						"vcpus":         schema.Int64Attribute{MarkdownDescription: "Number of vCPUs.", Computed: true},
						"memory_mb":     schema.Float64Attribute{MarkdownDescription: "Memory in MB.", Computed: true},
						"storage_gb":    schema.Float64Attribute{MarkdownDescription: "Storage in GB.", Computed: true},
						"id":            schema.StringAttribute{MarkdownDescription: "Specification identifier.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *specificationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *specificationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := d.client.ListAllSpecifications(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading specifications failed", err.Error())
		return
	}

	state := specificationsModel{Specifications: make([]specificationModel, 0, len(all))}
	for _, s := range all {
		state.Specifications = append(state.Specifications, fromAPI(s))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
