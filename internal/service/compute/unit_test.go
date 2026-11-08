package compute_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

func unitProviderBlock(url string) string {
	// Zero retry backoff: error-injection tests exercise 5xx responses, and the
	// default 1–30s exponential backoff would otherwise add ~15s per such test.
	// Retries still happen (just without the wait), so retry behaviour is intact.
	return fmt.Sprintf("provider \"strettchcloud\" {\n  api_key = \"unit-test\"\n  api_url = %q\n  http_retry_wait_min = 0\n  http_retry_wait_max = 0\n}\n", url)
}

func unitComputeConfig(url, host string) string {
	return unitProviderBlock(url) + fmt.Sprintf(`resource "strettchcloud_compute" "test" {
  host_name     = %q
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
}`, host)
}

// TestUnitResource_CreateError covers the Create error branch: POST /computes
// returns 400, so no polling happens and Create surfaces the diagnostic.
func TestUnitResource_CreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"code":"BAD_REQUEST","message":"bad key"}`))
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      unitComputeConfig(srv.URL, "x"),
			ExpectError: regexp.MustCompile("Creating compute failed"),
		}},
	})
}

// TestUnitDataSourceCompute covers the by-id data source success and 404 paths.
func TestUnitDataSourceCompute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/404") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"code":"RESOURCE_NOT_FOUND","message":"gone"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/500") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
			return
		}
		// withPublicIp is included so the data source's explicit-flag branch runs.
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"7","hostName":"h","state":"RUNNING","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G","ipv4":"1.2.3.4","privateIpv4":"10.0.0.1","withPublicIp":true,"tags":["a"],"createdAt":"t","updatedAt":"t"}}`))
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: unitProviderBlock(srv.URL) + `data "strettchcloud_compute" "t" { id = "7" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.strettchcloud_compute.t", "host_name", "h"),
					resource.TestCheckResourceAttr("data.strettchcloud_compute.t", "ipv4", "1.2.3.4"),
					resource.TestCheckResourceAttr("data.strettchcloud_compute.t", "with_public_ip", "true"),
				),
			},
			{
				Config:      unitProviderBlock(srv.URL) + `data "strettchcloud_compute" "t" { id = "404" }`,
				ExpectError: regexp.MustCompile("No matching compute"),
			},
			{
				Config:      unitProviderBlock(srv.URL) + `data "strettchcloud_compute" "t" { id = "500" }`,
				ExpectError: regexp.MustCompile("Reading compute failed"),
			},
		},
	})
}

// TestUnitList covers the list resource success path via `terraform query`
// against a mock /computes.
func TestUnitList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"1","state":"RUNNING","hostName":"a"},{"id":"2","state":"STOPPED","hostName":"b"}],"meta":{"total":2,"page":1,"limit":100}}`))
	}))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{
				Query: true,
				Config: unitProviderBlock(srv.URL) + `
list "strettchcloud_compute" "all" {
  provider = strettchcloud
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("strettchcloud_compute.all", 2),
				},
			},
			{
				// -generate-config-out sets IncludeResource, exercising the branch
				// that populates each result's resource attributes.
				Query:          true,
				GenerateConfig: true,
				Config: unitProviderBlock(srv.URL) + `
list "strettchcloud_compute" "all" {
  provider = strettchcloud
}
`,
			},
		},
	})
}

// TestUnitList_Error covers the List error path: GET /computes returns 500, so
// List emits a diagnostic.
func TestUnitList_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
	}))
	defer srv.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Query:       true,
			Config:      unitProviderBlock(srv.URL) + "list \"strettchcloud_compute\" \"all\" {\n  provider = strettchcloud\n}\n",
			ExpectError: regexp.MustCompile("Listing computes failed"),
		}},
	})
}

// rebootMock serves create + reboot; failReboot makes POST /reboot return 500.
func rebootMock(t *testing.T, failReboot bool) string {
	t.Helper()
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reboot"):
			if failReboot {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING"}}`))
		case r.Method == http.MethodDelete:
			deleted = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"DESTROYING"}}`))
		case r.Method == http.MethodGet && deleted:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"code":"RESOURCE_NOT_FOUND","message":"gone"}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING","hostName":"a","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func rebootConfig(url, timeout string) string {
	tline := ""
	if timeout != "" {
		tline = fmt.Sprintf("\n    timeout = %q", timeout)
	}
	return unitProviderBlock(url) + fmt.Sprintf(`
resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.strettchcloud_compute_reboot.test]
    }
  }
}
action "strettchcloud_compute_reboot" "test" {
  config {
    compute_id = strettchcloud_compute.test.id%s
  }
}
`, tline)
}

func TestUnitActionReboot_BadTimeout(t *testing.T) {
	url := rebootMock(t, false)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Config:      rebootConfig(url, "not-a-duration"),
			ExpectError: regexp.MustCompile("Invalid timeout"),
		}},
	})
}

func TestUnitActionReboot_RebootError(t *testing.T) {
	url := rebootMock(t, true)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Config:      rebootConfig(url, ""),
			ExpectError: regexp.MustCompile("Rebooting compute failed"),
		}},
	})
}

// TestUnitActionReboot covers the reboot action via an after_create trigger: the
// mock reports RUNNING so the precondition passes, reboot is issued, and the
// compute settles back to RUNNING.
func TestUnitActionReboot(t *testing.T) {
	var mu sync.Mutex
	rebooted := false
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/computes"):
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING","hostName":"a","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reboot"):
			mu.Lock()
			rebooted = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING"}}`))
		case r.Method == http.MethodDelete:
			mu.Lock()
			deleted = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"DESTROYING"}}`))
		default: // GET
			mu.Lock()
			gone := deleted
			wasRebooted := rebooted
			mu.Unlock()
			if gone {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"code":"RESOURCE_NOT_FOUND","message":"gone"}`))
				return
			}
			// updatedAt changes once the reboot is applied, so WaitForReboot can
			// tell the post-reboot RUNNING from the pre-reboot one.
			updated := "t1"
			if wasRebooted {
				updated = "t2"
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"id":"1","state":"RUNNING","hostName":"a","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G","updatedAt":%q}}`, updated)
		}
	}))
	defer srv.Close()

	config := unitProviderBlock(srv.URL) + `
resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.strettchcloud_compute_reboot.test]
    }
  }
}
action "strettchcloud_compute_reboot" "test" {
  config { compute_id = strettchcloud_compute.test.id }
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Config: config,
			Check: func(*terraform.State) error {
				mu.Lock()
				defer mu.Unlock()
				if !rebooted {
					return fmt.Errorf("reboot endpoint was not called")
				}
				return nil
			},
		}},
	})
}

// mockCompute is an in-memory compute the mock server mutates. It lets unit tests
// drive the full resource lifecycle — including the PATCH update endpoint that
// production does not yet implement — deterministically and without a real API.
type mockCompute struct {
	mu           sync.Mutex
	state        string
	spec         string
	host         string
	tags         []string
	gone         bool
	failUpdate   bool   // make the PATCH update endpoint return 500 (error injection)
	failUpsize   bool   // make the upsize endpoint return 500
	failDelete   bool   // make DELETE return 500
	failGet      bool   // make GET return 500
	failStop     bool   // make POST /stop return 500
	deleteNoGone bool   // DELETE returns 200 but the compute keeps reporting a live state
	createState  string // state POST /computes puts the compute in (default RUNNING)
}

func (m *mockCompute) body() string {
	tags, _ := json.Marshal(m.tags)
	return fmt.Sprintf(`{"success":true,"data":{"id":"1","state":%q,"hostName":%q,"image":"UBUNTU-24.04","region":"KGL-1","specification":%q,"tags":%s}}`,
		m.state, m.host, m.spec, tags)
}

func newMockServer(t *testing.T, initState, initSpec string) (*httptest.Server, *mockCompute) {
	t.Helper()
	m := &mockCompute{state: initState, spec: initSpec, host: "a", createState: "RUNNING"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		// Catalog endpoints — so ModifyPlan's plan-time validation sees valid slugs.
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/computes/specifications"):
			_, _ = w.Write([]byte(`{"success":true,"data":[` +
				`{"id":"a","specification":"S-1C-1G","vcpus":1,"memoryMb":1024,"storageGb":20},` +
				`{"id":"b","specification":"S-1C-2G","vcpus":1,"memoryMb":2048,"storageGb":40},` +
				`{"id":"c","specification":"S-2C-4G","vcpus":2,"memoryMb":4096,"storageGb":80},` +
				`{"id":"d","specification":"S-4C-8G","vcpus":4,"memoryMb":8192,"storageGb":160}` +
				`],"meta":{"total":4,"page":1,"limit":100}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/images"):
			_, _ = w.Write([]byte(`{"success":true,"data":[` +
				`{"image":"UBUNTU-24.04","imageName":"Ubuntu","version":"24.04","displayName":"Ubuntu 24.04"},` +
				`{"image":"DEBIAN-13","imageName":"Debian","version":"13","displayName":"Debian 13"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/regions"):
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":1,"region":"KGL-1","countryCode":"RW","name":"Kigali-1"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/computes"):
			m.state = m.createState
			m.gone = false // a replace recreates the compute
			_, _ = w.Write([]byte(m.body()))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
			if m.failStop {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			m.state = "STOPPED"
			_, _ = w.Write([]byte(m.body()))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			m.state = "RUNNING"
			_, _ = w.Write([]byte(m.body()))
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/upsize"):
			if m.failUpsize {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			var req struct {
				NewSpecification string `json:"newSpecification"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			m.spec = req.NewSpecification // stays STOPPED after upsize
			_, _ = w.Write([]byte(m.body()))
		case r.Method == http.MethodPatch: // PATCH /computes/{id} update
			if m.failUpdate {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			var req struct {
				HostName *string   `json:"hostName"`
				Tags     *[]string `json:"tags"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.HostName != nil {
				m.host = *req.HostName
			}
			if req.Tags != nil {
				m.tags = *req.Tags
			}
			_, _ = w.Write([]byte(m.body()))
		case r.Method == http.MethodDelete:
			if m.failDelete {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			if m.deleteNoGone {
				// DELETE accepted, but the compute never actually goes away: the
				// poll for DESTROYED will see a live stable state and error.
				_, _ = w.Write([]byte(m.body()))
				return
			}
			m.gone = true
			m.state = "DESTROYING"
			_, _ = w.Write([]byte(m.body()))
		default: // GET
			if m.failGet {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			if m.gone {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"code":"RESOURCE_NOT_FOUND","message":"gone"}`))
				return
			}
			_, _ = w.Write([]byte(m.body()))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func mockCfg(url, host, spec, state string) string {
	s := ""
	if state != "" {
		s = fmt.Sprintf("\n  state = %q", state)
	}
	return unitProviderBlock(url) + fmt.Sprintf(`resource "strettchcloud_compute" "test" {
  host_name     = %q
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = %q
  public_keys   = ["ssh-ed25519 AAAA"]%s
}`, host, spec, s)
}

// TestUnitLifecycle_CreateStoppedThenUpdateAndResize drives the paths production
// cannot: create with state=STOPPED, an in-place host_name/tags update (PATCH),
// and an upsize — all against the stateful mock.
func TestUnitLifecycle_CreateStoppedThenUpdateAndResize(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create with state=STOPPED exercises the stop-after-create branch.
				Config: mockCfg(srv.URL, "a", "S-1C-1G", "STOPPED"),
				Check:  resource.TestCheckResourceAttr("strettchcloud_compute.test", "state", "STOPPED"),
			},
			{
				// Start + change host_name and tags in place (PATCH update endpoint).
				Config: unitProviderBlock(srv.URL) + `resource "strettchcloud_compute" "test" {
  host_name     = "renamed"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
  tags          = ["prod"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("strettchcloud_compute.test", "host_name", "renamed"),
					resource.TestCheckResourceAttr("strettchcloud_compute.test", "state", "RUNNING"),
				),
			},
			{
				// Upsize (also emits the ModifyPlan stop/start warning).
				Config: mockCfg(srv.URL, "renamed", "S-1C-2G", ""),
				Check:  resource.TestCheckResourceAttr("strettchcloud_compute.test", "specification", "S-1C-2G"),
			},
		},
	})
}

// TestUnitLifecycle_PublicKeysReplace covers the public_keys RequiresReplaceIf
// plan modifier: changing public_keys on an existing compute forces a replace.
func TestUnitLifecycle_PublicKeysReplace(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")
	key2 := unitProviderBlock(srv.URL) + `resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 BBBB"]
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{Config: key2, Check: resource.TestCheckResourceAttr("strettchcloud_compute.test", "public_keys.#", "1")},
		},
	})
}

// TestUnitLifecycle_InvalidSpecRejected: an unknown specification slug fails at
// plan time (catalog validation), not at apply.
func TestUnitLifecycle_InvalidSpecRejected(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      mockCfg(srv.URL, "a", "NOPE-9C-9G", ""),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile("Unknown specification"),
		}},
	})
}

// TestUnitLifecycle_InvalidImageRejected: an unknown image slug fails at plan time.
func TestUnitLifecycle_InvalidImageRejected(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")
	cfg := unitProviderBlock(srv.URL) + `resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "NOT-A-REAL-IMAGE"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      cfg,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile("Unknown image"),
		}},
	})
}

// TestUnitLifecycle_InvalidRegionRejected: an unknown region slug fails at plan.
func TestUnitLifecycle_InvalidRegionRejected(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")
	cfg := unitProviderBlock(srv.URL) + `resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "NOWHERE-9"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      cfg,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile("Unknown region"),
		}},
	})
}

// TestUnitLifecycle_DownsizeRejected covers the ModifyPlan downsize error branch.
func TestUnitLifecycle_DownsizeRejected(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-2C-4G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-2C-4G", "")},
			{
				Config:      mockCfg(srv.URL, "a", "S-1C-1G", ""),
				ExpectError: regexp.MustCompile("Downsizing is not supported"),
			},
		},
	})
}

// TestUnitLifecycle_DownsizeAllowedOnReplace verifies the downsize rule does not
// fire when another changed field already forces a replace: the compute is
// recreated at the smaller size rather than resized in place.
func TestUnitLifecycle_DownsizeAllowedOnReplace(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-2C-4G")
	cfg := func(spec, key string) string {
		return unitProviderBlock(srv.URL) + fmt.Sprintf(`resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = %q
  public_keys   = [%q]
}`, spec, key)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("S-2C-4G", "ssh-ed25519 AAAA")},
			{
				// A smaller spec together with a changed public_keys: the key change
				// forces a replace, so there is no "Downsizing is not supported"
				// error — just a (non-empty) replacement plan.
				Config:             cfg("S-1C-1G", "ssh-ed25519 BBBB"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestUnitLifecycle_CreateSettleError covers Create's settle error branch: the
// POST succeeds but the compute reports DESTROYED, so the poll to RUNNING fails.
func TestUnitLifecycle_CreateSettleError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	m.mu.Lock()
	m.createState = "DESTROYED"
	m.mu.Unlock()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      mockCfg(srv.URL, "a", "S-1C-1G", ""),
			ExpectError: regexp.MustCompile("Compute did not reach RUNNING"),
		}},
	})
}

// TestUnitLifecycle_UpdateReconcileError covers Update's state-reconcile error
// branch: changing state to STOPPED when the stop call fails.
func TestUnitLifecycle_UpdateReconcileError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				PreConfig:   func() { m.mu.Lock(); m.failStop = true; m.mu.Unlock() },
				Config:      mockCfg(srv.URL, "a", "S-1C-1G", "STOPPED"),
				ExpectError: regexp.MustCompile("Compute did not reach STOPPED"),
			},
			{
				PreConfig: func() { m.mu.Lock(); m.failStop = false; m.mu.Unlock() },
				Config:    mockCfg(srv.URL, "a", "S-1C-1G", ""),
			},
		},
	})
}

// TestUnitLifecycle_ResizeError covers the Update resize error branch when upsize
// fails.
func TestUnitLifecycle_ResizeError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				PreConfig:   func() { m.mu.Lock(); m.failUpsize = true; m.mu.Unlock() },
				Config:      mockCfg(srv.URL, "a", "S-1C-2G", ""),
				ExpectError: regexp.MustCompile("Resizing compute failed"),
			},
		},
	})
}

// TestUnitLifecycle_DeleteError covers the Delete error branch via a destroy step
// that the mock rejects.
func TestUnitLifecycle_DeleteError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				PreConfig:   func() { m.mu.Lock(); m.failDelete = true; m.mu.Unlock() },
				Config:      mockCfg(srv.URL, "a", "S-1C-1G", ""),
				Destroy:     true,
				ExpectError: regexp.MustCompile("Deleting compute failed"),
			},
			{
				// Re-enable delete so the harness can clean up.
				PreConfig: func() { m.mu.Lock(); m.failDelete = false; m.mu.Unlock() },
				Config:    mockCfg(srv.URL, "a", "S-1C-1G", ""),
			},
		},
	})
}

// TestUnitLifecycle_Read covers the Read removal and error branches: a compute
// deleted out-of-band (404) is removed from state; a DESTROYED compute is removed;
// a non-404 error surfaces.
func TestUnitLifecycle_ReadRemoval(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				// Compute vanishes out-of-band -> refresh removes it -> plan recreates.
				PreConfig:          func() { m.mu.Lock(); m.gone = true; m.mu.Unlock() },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
			{
				PreConfig: func() { m.mu.Lock(); m.gone = false; m.mu.Unlock() },
				Config:    mockCfg(srv.URL, "a", "S-1C-1G", ""),
			},
		},
	})
}

func TestUnitLifecycle_ReadDestroyed(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				// A DESTROYED state is treated as gone.
				PreConfig:          func() { m.mu.Lock(); m.state = "DESTROYED"; m.mu.Unlock() },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
			{
				PreConfig: func() { m.mu.Lock(); m.state = "RUNNING"; m.mu.Unlock() },
				Config:    mockCfg(srv.URL, "a", "S-1C-1G", ""),
			},
		},
	})
}

func TestUnitLifecycle_ReadError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				PreConfig:    func() { m.mu.Lock(); m.failGet = true; m.mu.Unlock() },
				RefreshState: true,
				ExpectError:  regexp.MustCompile("Reading compute failed"),
			},
			{
				PreConfig: func() { m.mu.Lock(); m.failGet = false; m.mu.Unlock() },
				Config:    mockCfg(srv.URL, "a", "S-1C-1G", ""),
			},
		},
	})
}

// TestUnitLifecycle_DeleteWaitError covers the branch where DELETE is accepted
// but the compute never reaches DESTROYED.
func TestUnitLifecycle_DeleteWaitError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	// A short delete timeout: when the compute never leaves its live state, the
	// wait for DESTROYED runs out the deadline instead of blocking on the default.
	cfg := unitProviderBlock(srv.URL) + `resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
  timeouts = { delete = "5s" }
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig:   func() { m.mu.Lock(); m.deleteNoGone = true; m.mu.Unlock() },
				Config:      cfg,
				Destroy:     true,
				ExpectError: regexp.MustCompile("Compute was not destroyed"),
			},
			{
				PreConfig: func() { m.mu.Lock(); m.deleteNoGone = false; m.mu.Unlock() },
				Config:    cfg,
			},
		},
	})
}

// TestUnitLifecycle_RebootPrecondition covers the action precondition: rebooting
// a STOPPED compute is rejected before the API is called.
func TestUnitLifecycle_RebootPrecondition(t *testing.T) {
	srv, _ := newMockServer(t, "RUNNING", "S-1C-1G")
	config := unitProviderBlock(srv.URL) + `
resource "strettchcloud_compute" "test" {
  host_name     = "a"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA"]
  state         = "STOPPED"
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.strettchcloud_compute_reboot.test]
    }
  }
}
action "strettchcloud_compute_reboot" "test" {
  config { compute_id = strettchcloud_compute.test.id }
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile("Compute is not running"),
		}},
	})
}

// TestUnitLifecycle_UpdateError covers the Update error branch when the PATCH
// update endpoint fails.
func TestUnitLifecycle_UpdateError(t *testing.T) {
	srv, m := newMockServer(t, "RUNNING", "S-1C-1G")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: mockCfg(srv.URL, "a", "S-1C-1G", "")},
			{
				PreConfig:   func() { m.mu.Lock(); m.failUpdate = true; m.mu.Unlock() },
				Config:      mockCfg(srv.URL, "renamed", "S-1C-1G", ""),
				ExpectError: regexp.MustCompile("Updating compute failed"),
			},
		},
	})
}
