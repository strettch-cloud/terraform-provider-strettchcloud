package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccProtoV6ProviderFactories is referenced by acceptance tests in other
// files; keep it exercised so the unused check stays quiet in this package.
var _ = testAccProtoV6ProviderFactories

// testAccProtoV6ProviderFactories registers the provider with the acceptance-test
// harness as "strettchcloud".
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"strettchcloud": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProvider_Metadata(t *testing.T) {
	t.Parallel()
	p := New("1.2.3")()
	var resp provider.MetadataResponse
	p.Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.TypeName != "strettchcloud" {
		t.Errorf("TypeName = %q, want strettchcloud", resp.TypeName)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", resp.Version)
	}
}

func TestStringOrEnv(t *testing.T) {
	t.Setenv(envAPIKey, "from-env")
	if got := stringOrEnv(types.StringNull(), envAPIKey); got != "from-env" {
		t.Errorf("null config should fall back to env, got %q", got)
	}
	if got := stringOrEnv(types.StringValue("from-config"), envAPIKey); got != "from-config" {
		t.Errorf("config value should win, got %q", got)
	}
}

func TestFloat64OrEnv(t *testing.T) {
	t.Setenv(envRequestsPerSecond, "1.5")
	if got := float64OrEnv(types.Float64Null(), envRequestsPerSecond, defaultRequestsPerSecond); got != 1.5 {
		t.Errorf("env fallback = %v, want 1.5", got)
	}
	if got := float64OrEnv(types.Float64Null(), "STRETTCH_CLOUD_UNSET_VAR", defaultRequestsPerSecond); got != defaultRequestsPerSecond {
		t.Errorf("default = %v, want %v", got, defaultRequestsPerSecond)
	}
	if got := float64OrEnv(types.Float64Value(2), envRequestsPerSecond, defaultRequestsPerSecond); got != 2 {
		t.Errorf("config value should win, got %v", got)
	}
}

func TestInt64OrEnv(t *testing.T) {
	t.Setenv(envHTTPRetryMax, "7")
	if got := int64OrEnv(types.Int64Null(), envHTTPRetryMax, defaultHTTPRetryMax); got != 7 {
		t.Errorf("env fallback = %d, want 7", got)
	}
	if got := int64OrEnv(types.Int64Value(3), envHTTPRetryMax, defaultHTTPRetryMax); got != 3 {
		t.Errorf("config value should win, got %d", got)
	}
}

// TestConfigureGuard covers the diagnostics guard after Config.Get in Configure
// by feeding a malformed configuration value.
func TestConfigureGuard(t *testing.T) {
	ctx := context.Background()
	p := New("test")()
	var sr provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sr)
	resp := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{
		Config: tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(tftypes.String, "nope")},
	}, resp)
	if !resp.Diagnostics.HasError() {
		t.Error("malformed config should produce an error")
	}
}

func TestProvider_SchemaAndRegistration(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var sc provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sc)
	if _, ok := sc.Schema.Attributes["api_key"]; !ok {
		t.Error("schema missing api_key")
	}

	if len(p.Resources(ctx)) == 0 {
		t.Error("no resources registered")
	}
	if len(p.DataSources(ctx)) != 7 {
		t.Errorf("want 7 data sources, got %d", len(p.DataSources(ctx)))
	}
	lp := p.(provider.ProviderWithListResources)
	if len(lp.ListResources(ctx)) == 0 {
		t.Error("no list resources")
	}
	ap := p.(provider.ProviderWithActions)
	if len(ap.Actions(ctx)) == 0 {
		t.Error("no actions")
	}
}

func TestProviderConfigure_MissingAPIKey(t *testing.T) {
	t.Setenv("STRETTCH_CLOUD_API_KEY", "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
provider "strettchcloud" {}
data "strettchcloud_region" "t" { region = "KGL-1" }
`,
			ExpectError: regexp.MustCompile("Missing API key"),
		}},
	})
}

func TestProviderConfigure_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":1,"region":"KGL-1","countryCode":"RW","name":"Kigali-1"}]}`))
	}))
	defer srv.Close()

	// Exercises the full Configure success path (env fallback, tuning knobs,
	// client build) including the optional tuning attributes.
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "strettchcloud" {
  api_key             = "unit-test"
  api_url             = %q
  requests_per_second = 2
  http_retry_max      = 2
  http_retry_wait_min = 1
  http_retry_wait_max = 5
}
data "strettchcloud_region" "t" { region = "KGL-1" }
`, srv.URL),
			Check: resource.TestCheckResourceAttr("data.strettchcloud_region.t", "country_code", "RW"),
		}},
	})
}
