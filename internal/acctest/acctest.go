// Package acctest holds the helpers every service package's acceptance tests
// share: provider factories, a PreCheck for required env vars, an env-built
// client (for CheckDestroy and sweepers), and the tag that marks test resources.
//
// Tests live in external _test packages; the factories live here, not in each
// package, to avoid an import cycle with internal/provider.
package acctest

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/provider"
)

const (
	// TestTag is an additional marker the sweeper recognises on test computes.
	TestTag = "tf-acc-test"

	// TestNamePrefix is the host_name prefix for test computes; each test appends
	// a random suffix to avoid clashes. It is the primary signal the sweeper uses
	// to identify leftover test resources.
	TestNamePrefix = "tf-acc-test-"

	envAPIKey = "STRETTCH_CLOUD_API_KEY"
)

// ProtoV6ProviderFactories registers the provider with the plugin-testing
// harness as "strettchcloud".
var ProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"strettchcloud": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// PreCheck runs before each acceptance test and fails fast if the API key is
// missing. The harness checks TF_ACC itself.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv(envAPIKey) == "" {
		t.Fatalf("%s must be set for acceptance tests", envAPIKey)
	}
}

// NewClientFromEnv builds an API client from the same env vars as the provider,
// for use in CheckDestroy and sweepers.
func NewClientFromEnv() *client.Client {
	return client.NewClient(client.Config{
		APIKey:       os.Getenv(envAPIKey),
		BaseURL:      os.Getenv("STRETTCH_CLOUD_API_URL"),
		UserAgent:    "terraform-provider-strettchcloud/acctest",
		RetryMax:     4,
		RetryWaitMin: time.Second,
		RetryWaitMax: 30 * time.Second,
	})
}

// IsTestResource reports whether a compute was created by the acceptance suite,
// so the sweeper can only ever delete test resources, never real ones. A compute
// qualifies if its host name has the test prefix (every test compute uses it) or
// it carries the test tag.
func IsTestResource(hostName string, tags []string) bool {
	if strings.HasPrefix(hostName, TestNamePrefix) {
		return true
	}
	for _, tag := range tags {
		if tag == TestTag {
			return true
		}
	}
	return false
}
