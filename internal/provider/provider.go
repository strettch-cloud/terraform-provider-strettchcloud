// Package provider is the Terraform layer's entry point. Configure() reads the
// provider config, builds one client.Client, and hands it to every resource and
// data source via the Framework's ProviderData. This package and
// internal/service are the only code that touches schema, plan or state; they
// never build HTTP requests.
package provider

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/service/compute"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/service/image"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/service/region"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/service/specification"
)

// Ensure the provider satisfies the Framework interfaces it implements.
var (
	_ provider.Provider                  = &strettchcloudProvider{}
	_ provider.ProviderWithListResources = &strettchcloudProvider{}
	_ provider.ProviderWithActions       = &strettchcloudProvider{}
)

// Environment-variable fallbacks and defaults.
const (
	envAPIKey            = "STRETTCH_CLOUD_API_KEY"
	envAPIURL            = "STRETTCH_CLOUD_API_URL"
	envRequestsPerSecond = "STRETTCH_CLOUD_REQUESTS_PER_SECOND"
	envHTTPRetryMax      = "STRETTCH_CLOUD_HTTP_RETRY_MAX"
	envHTTPRetryWaitMin  = "STRETTCH_CLOUD_HTTP_RETRY_WAIT_MIN"
	envHTTPRetryWaitMax  = "STRETTCH_CLOUD_HTTP_RETRY_WAIT_MAX"

	defaultRequestsPerSecond = 2.0
	defaultHTTPRetryMax      = 4
	defaultHTTPRetryWaitMin  = 1.0  // seconds
	defaultHTTPRetryWaitMax  = 30.0 // seconds
)

// strettchcloudProvider is the provider implementation. version is injected at
// build time and feeds the User-Agent.
type strettchcloudProvider struct {
	version string
}

// providerModel is the decoded provider configuration block.
type providerModel struct {
	APIKey            types.String  `tfsdk:"api_key"`
	APIURL            types.String  `tfsdk:"api_url"`
	RequestsPerSecond types.Float64 `tfsdk:"requests_per_second"`
	HTTPRetryMax      types.Int64   `tfsdk:"http_retry_max"`
	HTTPRetryWaitMin  types.Float64 `tfsdk:"http_retry_wait_min"`
	HTTPRetryWaitMax  types.Float64 `tfsdk:"http_retry_wait_max"`
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &strettchcloudProvider{version: version}
	}
}

func (p *strettchcloudProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "strettchcloud"
	resp.Version = p.version
}

func (p *strettchcloudProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Strettch Cloud provider lets you manage your Strettch Cloud infrastructure with Terraform.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "API key for authentication, sent as `Authorization: Bearer <key>`. May also be set with the `STRETTCH_CLOUD_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"api_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the Strettch Cloud API. Defaults to the production URL. May also be set with the `STRETTCH_CLOUD_API_URL` environment variable.",
				Optional:            true,
			},
			"requests_per_second": schema.Float64Attribute{
				MarkdownDescription: "Client-side rate limit, in requests per second. Defaults to `2`, the API ceiling; higher values are capped to `2`. May also be set with `STRETTCH_CLOUD_REQUESTS_PER_SECOND`.",
				Optional:            true,
			},
			"http_retry_max": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of retries after the first attempt for retryable failures. Defaults to `4`. May also be set with `STRETTCH_CLOUD_HTTP_RETRY_MAX`.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"http_retry_wait_min": schema.Float64Attribute{
				MarkdownDescription: "Minimum backoff, in seconds, before the first retry. Defaults to `1`. May also be set with `STRETTCH_CLOUD_HTTP_RETRY_WAIT_MIN`.",
				Optional:            true,
			},
			"http_retry_wait_max": schema.Float64Attribute{
				MarkdownDescription: "Maximum backoff, in seconds, between retries. Defaults to `30`. May also be set with `STRETTCH_CLOUD_HTTP_RETRY_WAIT_MAX`.",
				Optional:            true,
			},
		},
	}
}

func (p *strettchcloudProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// api_key: config value, then env, else error.
	apiKey := stringOrEnv(cfg.APIKey, envAPIKey)
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing API key",
			"Set the api_key provider argument or the "+envAPIKey+" environment variable.",
		)
		return
	}

	// api_url: config value, then env. An empty value falls back to the default
	// production URL (https) in the client.
	apiURL := stringOrEnv(cfg.APIURL, envAPIURL)

	rps := float64OrEnv(cfg.RequestsPerSecond, envRequestsPerSecond, defaultRequestsPerSecond)
	retryMax := int(int64OrEnv(cfg.HTTPRetryMax, envHTTPRetryMax, defaultHTTPRetryMax))
	retryWaitMin := float64OrEnv(cfg.HTTPRetryWaitMin, envHTTPRetryWaitMin, defaultHTTPRetryWaitMin)
	retryWaitMax := float64OrEnv(cfg.HTTPRetryWaitMax, envHTTPRetryWaitMax, defaultHTTPRetryWaitMax)

	userAgent := "terraform-provider-strettchcloud/" + p.version + " Terraform/" + req.TerraformVersion

	c := client.NewClient(client.Config{
		APIKey:            apiKey,
		BaseURL:           apiURL,
		UserAgent:         userAgent,
		RequestsPerSecond: rps,
		RetryMax:          retryMax,
		RetryWaitMin:      secondsToDuration(retryWaitMin),
		RetryWaitMax:      secondsToDuration(retryWaitMax),
	})

	// Hand the client to every resource, data source, list resource and action.
	resp.ResourceData = c
	resp.DataSourceData = c
	resp.EphemeralResourceData = c
	resp.ActionData = c
	resp.ListResourceData = c
}

func (p *strettchcloudProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		compute.NewResource,
	}
}

func (p *strettchcloudProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		specification.NewDataSource,
		specification.NewListDataSource,
		image.NewDataSource,
		image.NewListDataSource,
		region.NewDataSource,
		region.NewListDataSource,
		compute.NewDataSource,
	}
}

func (p *strettchcloudProvider) ListResources(_ context.Context) []func() list.ListResource {
	return []func() list.ListResource{
		compute.NewListResource,
	}
}

func (p *strettchcloudProvider) Actions(_ context.Context) []func() action.Action {
	return []func() action.Action{
		compute.NewRebootAction,
	}
}

func secondsToDuration(sec float64) time.Duration {
	return time.Duration(sec * float64(time.Second))
}

// stringOrEnv returns the config value if set, else the environment variable.
func stringOrEnv(v types.String, env string) string {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueString()
	}
	return os.Getenv(env)
}

// float64OrEnv returns the config value if set, else the parsed env var, else
// the default.
func float64OrEnv(v types.Float64, env string, def float64) float64 {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueFloat64()
	}
	if s := os.Getenv(env); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return def
}

// int64OrEnv returns the config value if set, else the parsed env var, else the
// default.
func int64OrEnv(v types.Int64, env string, def int64) int64 {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueInt64()
	}
	if s := os.Getenv(env); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return def
}
