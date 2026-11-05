package image

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

var (
	// strettchcloud_image (singular, by filter)
	_ datasource.DataSource                     = &imageDataSource{}
	_ datasource.DataSourceWithConfigure        = &imageDataSource{}
	_ datasource.DataSourceWithConfigValidators = &imageDataSource{}

	// strettchcloud_images (plural, all)
	_ datasource.DataSource              = &imagesDataSource{}
	_ datasource.DataSourceWithConfigure = &imagesDataSource{}
)

// NewDataSource returns the singular strettchcloud_image data source.
func NewDataSource() datasource.DataSource {
	return &imageDataSource{}
}

type imageDataSource struct {
	client *client.Client
}

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Finds a single OS image by filter. At least one filter is required; filters combine with AND. Zero matches or more than one match is an error when read.",
		Attributes: map[string]schema.Attribute{
			"image": schema.StringAttribute{
				MarkdownDescription: "Image slug, e.g. `UBUNTU-24.04`. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"image_name": schema.StringAttribute{
				MarkdownDescription: "Image name, e.g. `Ubuntu`. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Image version, e.g. `24.04`. Filter and output.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Human-friendly image name. Output only.",
				Computed:            true,
			},
		},
	}
}

func (d *imageDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(
			path.MatchRoot("image"),
			path.MatchRoot("image_name"),
			path.MatchRoot("version"),
		),
	}
}

func (d *imageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg imageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.client.ListImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading images failed", err.Error())
		return
	}

	matches := matchImages(all, cfg)
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No matching image",
			fmt.Sprintf("No image matches the given filters (%s). Valid images: %s.", describeFilters(cfg), imageSlugs(all)))
	case 1:
		resp.Diagnostics.Append(resp.State.Set(ctx, fromAPI(matches[0]))...)
	default:
		resp.Diagnostics.AddError("Ambiguous image", fmt.Sprintf("%d images match the given filters; add more filters to select exactly one.", len(matches)))
	}
}

// matchImages returns the images satisfying every provided filter (AND). It is
// a pure function so the 0/1/many behaviour can be unit-tested.
func matchImages(all []client.Image, cfg imageModel) []client.Image {
	matches := make([]client.Image, 0, len(all))
	for _, i := range all {
		if !cfg.Image.IsNull() && i.Image != cfg.Image.ValueString() {
			continue
		}
		if !cfg.ImageName.IsNull() && i.ImageName != cfg.ImageName.ValueString() {
			continue
		}
		if !cfg.Version.IsNull() && i.Version != cfg.Version.ValueString() {
			continue
		}
		matches = append(matches, i)
	}
	return matches
}

// imageSlugs returns the catalog's image slugs, sorted and comma-joined, for a
// readable "valid images: ..." suggestion in the no-match error.
func imageSlugs(all []client.Image) string {
	slugs := make([]string, len(all))
	for i, im := range all {
		slugs[i] = im.Image
	}
	sort.Strings(slugs)
	return strings.Join(slugs, ", ")
}

func describeFilters(cfg imageModel) string {
	var parts []string
	if !cfg.Image.IsNull() {
		parts = append(parts, "image="+cfg.Image.ValueString())
	}
	if !cfg.ImageName.IsNull() {
		parts = append(parts, "image_name="+cfg.ImageName.ValueString())
	}
	if !cfg.Version.IsNull() {
		parts = append(parts, "version="+cfg.Version.ValueString())
	}
	return strings.Join(parts, ", ")
}

// NewListDataSource returns the plural strettchcloud_images data source.
func NewListDataSource() datasource.DataSource {
	return &imagesDataSource{}
}

type imagesDataSource struct {
	client *client.Client
}

type imagesModel struct {
	Images []imageModel `tfsdk:"images"`
}

func (d *imagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_images"
}

func (d *imagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns every OS image in the catalog.",
		Attributes: map[string]schema.Attribute{
			"images": schema.ListNestedAttribute{
				MarkdownDescription: "All OS images.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"image":        schema.StringAttribute{MarkdownDescription: "Image slug.", Computed: true},
						"image_name":   schema.StringAttribute{MarkdownDescription: "Image name.", Computed: true},
						"version":      schema.StringAttribute{MarkdownDescription: "Image version.", Computed: true},
						"display_name": schema.StringAttribute{MarkdownDescription: "Human-friendly image name.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *imagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = svcutil.DataSourceClient(req, resp)
}

func (d *imagesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := d.client.ListImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading images failed", err.Error())
		return
	}

	state := imagesModel{Images: make([]imageModel, 0, len(all))}
	for _, i := range all {
		state.Images = append(state.Images, fromAPI(i))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
