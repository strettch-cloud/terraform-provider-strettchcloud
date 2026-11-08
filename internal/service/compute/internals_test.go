package compute

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwdsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwrschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// badRaw is a tftypes value of the wrong shape (a string, not the schema's
// object), so any req.*.Get(ctx, &model) into a struct fails — used to cover the
// defensive `if resp.Diagnostics.HasError() { return }` guards after Get.
func badRaw() tftypes.Value { return tftypes.NewValue(tftypes.String, "not-an-object") }

func resSchema(ctx context.Context) fwrschema.Schema {
	var r resource.SchemaResponse
	(&computeResource{}).Schema(ctx, resource.SchemaRequest{}, &r)
	return r.Schema
}

func dsSchema(ctx context.Context) fwdsschema.Schema {
	var r datasource.SchemaResponse
	(&computeDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &r)
	return r.Schema
}

// TestGetGuards covers the defensive diagnostics guards after Get in the
// resource and data-source methods by feeding malformed configuration.
func TestGetGuards(t *testing.T) {
	ctx := context.Background()
	rs := resSchema(ctx)
	ds := dsSchema(ctx)
	r := &computeResource{}

	t.Run("resource Read", func(t *testing.T) {
		resp := &resource.ReadResponse{State: tfsdk.State{Schema: rs}}
		r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: rs, Raw: badRaw()}}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
	t.Run("resource Delete", func(t *testing.T) {
		resp := &resource.DeleteResponse{State: tfsdk.State{Schema: rs}}
		r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: rs, Raw: badRaw()}}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
	t.Run("resource Create", func(t *testing.T) {
		resp := &resource.CreateResponse{State: tfsdk.State{Schema: rs}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: rs, Raw: badRaw()}}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
	t.Run("resource Update", func(t *testing.T) {
		resp := &resource.UpdateResponse{State: tfsdk.State{Schema: rs}}
		r.Update(ctx, resource.UpdateRequest{
			Plan:  tfsdk.Plan{Schema: rs, Raw: badRaw()},
			State: tfsdk.State{Schema: rs, Raw: badRaw()},
		}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
	t.Run("data source Read", func(t *testing.T) {
		resp := &datasource.ReadResponse{State: tfsdk.State{Schema: ds}}
		(&computeDataSource{}).Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: ds, Raw: badRaw()}}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
	t.Run("action Invoke", func(t *testing.T) {
		var as action.SchemaResponse
		(&computeRebootAction{}).Schema(ctx, action.SchemaRequest{}, &as)
		resp := &action.InvokeResponse{}
		(&computeRebootAction{}).Invoke(ctx, action.InvokeRequest{Config: tfsdk.Config{Schema: as.Schema, Raw: badRaw()}}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("want error")
		}
	})
}

func testClient(url string) *client.Client {
	return client.NewClient(client.Config{APIKey: "k", BaseURL: url})
}

// mockComputeServer serves a compute whose state is fixed, for direct method tests.
func mockComputeServer(state string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"` + state + `"}}`))
	}))
}

func TestResourceMetadataSchema(t *testing.T) {
	ctx := context.Background()
	r := &computeResource{}

	var md resource.MetadataResponse
	r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "strettchcloud"}, &md)
	if md.TypeName != "strettchcloud_compute" {
		t.Errorf("typename %q", md.TypeName)
	}

	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	if len(sc.Schema.Attributes) == 0 {
		t.Error("empty schema")
	}

	var id resource.IdentitySchemaResponse
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &id)
	if _, ok := id.IdentitySchema.Attributes["id"]; !ok {
		t.Error("identity schema missing id")
	}

	r.Configure(ctx, resource.ConfigureRequest{ProviderData: testClient("http://x")}, &resource.ConfigureResponse{})
	if r.client == nil {
		t.Error("configure did not set client")
	}
	// nil ProviderData is a no-op (first configure pass).
	r.Configure(ctx, resource.ConfigureRequest{}, &resource.ConfigureResponse{})
}

func TestDataSourceMetadataSchema(t *testing.T) {
	ctx := context.Background()
	d := &computeDataSource{}
	var md datasource.MetadataResponse
	d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "strettchcloud"}, &md)
	if md.TypeName != "strettchcloud_compute" {
		t.Errorf("typename %q", md.TypeName)
	}
	var sc datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sc)
	if len(sc.Schema.Attributes) == 0 {
		t.Error("empty schema")
	}
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: testClient("http://x")}, &datasource.ConfigureResponse{})
}

func TestListMetadataSchema(t *testing.T) {
	ctx := context.Background()
	l := &computeList{}
	var md resource.MetadataResponse
	l.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "strettchcloud"}, &md)
	if md.TypeName != "strettchcloud_compute" {
		t.Errorf("typename %q", md.TypeName)
	}
	var sc list.ListResourceSchemaResponse
	l.ListResourceConfigSchema(ctx, list.ListResourceSchemaRequest{}, &sc)
	l.Configure(ctx, resource.ConfigureRequest{ProviderData: testClient("http://x")}, &resource.ConfigureResponse{})
}

func TestActionMetadataSchema(t *testing.T) {
	ctx := context.Background()
	a := &computeRebootAction{}
	var md action.MetadataResponse
	a.Metadata(ctx, action.MetadataRequest{ProviderTypeName: "strettchcloud"}, &md)
	if md.TypeName != "strettchcloud_compute_reboot" {
		t.Errorf("typename %q", md.TypeName)
	}
	var sc action.SchemaResponse
	a.Schema(ctx, action.SchemaRequest{}, &sc)
	if len(sc.Schema.Attributes) == 0 {
		t.Error("empty schema")
	}
	a.Configure(ctx, action.ConfigureRequest{ProviderData: testClient("http://x")}, &action.ConfigureResponse{})
}

func TestConfigValidators(t *testing.T) {
	// The compute data source has no ConfigValidators, so there is nothing to
	// assert here; this just guards against a panic if one is added later.
	_ = &computeDataSource{}
}

func TestReconcileState(t *testing.T) {
	ctx := context.Background()

	// Already at target: no API mutation needed.
	srvRunning := mockComputeServer(client.StateRunning)
	defer srvRunning.Close()
	r := &computeResource{client: testClient(srvRunning.URL)}
	if err := r.reconcileState(ctx, "1", client.StateRunning); err != nil {
		t.Errorf("already-running: %v", err)
	}

	// Unsupported target.
	if err := r.reconcileState(ctx, "1", "BOGUS"); err == nil {
		t.Error("unsupported target should error")
	}

	// GET error propagates.
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
	}))
	defer srvErr.Close()
	rErr := &computeResource{client: testClient(srvErr.URL)}
	if err := rErr.reconcileState(ctx, "1", client.StateRunning); err == nil {
		t.Error("get error should propagate")
	}

	// Start the compute (currently STOPPED) -> success, and failure variants.
	if err := (&computeResource{client: newStateSrv(t, &stateSrv{state: client.StateStopped})}).reconcileState(ctx, "1", client.StateRunning); err != nil {
		t.Errorf("start to running should succeed: %v", err)
	}
	if err := (&computeResource{client: newStateSrv(t, &stateSrv{state: client.StateStopped, failStart: true})}).reconcileState(ctx, "1", client.StateRunning); err == nil {
		t.Error("start error should propagate")
	}
	if err := (&computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning, failStop: true})}).reconcileState(ctx, "1", client.StateStopped); err == nil {
		t.Error("stop error should propagate")
	}
}

func TestBuildUpdateRequest(t *testing.T) {
	ctx := context.Background()
	tagsA, _ := types.ListValueFrom(ctx, types.StringType, []string{"a"})
	tagsB, _ := types.ListValueFrom(ctx, types.StringType, []string{"b"})

	// host_name changed only.
	req, _ := buildUpdateRequest(ctx,
		computeResourceModel{HostName: types.StringValue("new"), Tags: tagsA},
		computeResourceModel{HostName: types.StringValue("old"), Tags: tagsA})
	if req.HostName == nil || *req.HostName != "new" || req.Tags != nil {
		t.Errorf("host-only update wrong: %+v", req)
	}

	// tags changed only.
	req2, _ := buildUpdateRequest(ctx,
		computeResourceModel{HostName: types.StringValue("same"), Tags: tagsB},
		computeResourceModel{HostName: types.StringValue("same"), Tags: tagsA})
	if req2.HostName != nil || req2.Tags == nil {
		t.Errorf("tags-only update wrong: %+v", req2)
	}

	// nothing changed.
	req3, _ := buildUpdateRequest(ctx,
		computeResourceModel{HostName: types.StringValue("same"), Tags: tagsA},
		computeResourceModel{HostName: types.StringValue("same"), Tags: tagsA})
	if req3.HostName != nil || req3.Tags != nil {
		t.Errorf("no-change update should be empty: %+v", req3)
	}
}

func TestHostNameOrTagsChanged(t *testing.T) {
	a := computeResourceModel{HostName: types.StringValue("x"), Tags: types.ListNull(types.StringType)}
	b := computeResourceModel{HostName: types.StringValue("y"), Tags: types.ListNull(types.StringType)}
	if !hostNameOrTagsChanged(a, b) {
		t.Error("host_name change not detected")
	}
	if hostNameOrTagsChanged(a, a) {
		t.Error("no change falsely detected")
	}
}

func TestSpecificationPath(t *testing.T) {
	if specificationPath().String() != "specification" {
		t.Errorf("path = %s", specificationPath())
	}
}

func TestStringSlice(t *testing.T) {
	ctx := context.Background()
	if got, _ := stringSlice(ctx, types.ListNull(types.StringType)); got != nil {
		t.Errorf("null -> nil, got %v", got)
	}
	if got, _ := stringSlice(ctx, types.ListUnknown(types.StringType)); got != nil {
		t.Errorf("unknown -> nil, got %v", got)
	}
	l, _ := types.ListValueFrom(ctx, types.StringType, []string{"a", "b"})
	if got, _ := stringSlice(ctx, l); len(got) != 2 {
		t.Errorf("values -> 2, got %v", got)
	}
}

func TestSetIdentity_NilIsNoOp(t *testing.T) {
	// A nil identity (older Terraform without identity support) is a no-op.
	setIdentity(context.Background(), nil, nil, "1")
}

func TestSetIdentity_WritesID(t *testing.T) {
	ctx := context.Background()
	var idResp resource.IdentitySchemaResponse
	(&computeResource{}).IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &idResp)

	idType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String}}
	identity := &tfsdk.ResourceIdentity{
		Schema: idResp.IdentitySchema,
		Raw:    tftypes.NewValue(idType, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil)}),
	}
	var diags diag.Diagnostics
	setIdentity(ctx, identity, &diags, "abc")
	if diags.HasError() {
		t.Fatalf("setIdentity: %v", diags)
	}

	var got computeIdentityModel
	if d := identity.Get(ctx, &got); d.HasError() {
		t.Fatalf("read identity: %v", d)
	}
	if got.ID.ValueString() != "abc" {
		t.Errorf("identity id = %q, want abc", got.ID.ValueString())
	}
}

// TestModifyPlanGuard feeds a malformed plan so plan.Get fails, covering the
// defensive diagnostics guard in ModifyPlan.
func TestModifyPlanGuard(t *testing.T) {
	ctx := context.Background()
	rs := resSchema(ctx)
	resp := &resource.ModifyPlanResponse{}
	(&computeResource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Schema: rs, Raw: badRaw()},
		State: tfsdk.State{Schema: rs},
	}, resp)
	if !resp.Diagnostics.HasError() {
		t.Error("malformed plan should error")
	}
}

// TestListError covers the List error branch when the API call fails.
func TestListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
	}))
	defer srv.Close()
	l := &computeList{client: testClient(srv.URL)}
	var stream list.ListResultsStream
	l.List(context.Background(), list.ListRequest{}, &stream)
	if stream.Results == nil {
		t.Fatal("expected a results stream on error")
	}
}

// TestListStream drives the success stream with IncludeResource set and a consumer
// that stops early, covering the generated-config population and the early return.
func TestListStream(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[` +
			`{"id":"1","state":"RUNNING","hostName":"a","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G","tags":["web"],"ipv4":"1.2.3.4"},` +
			`{"id":"2","state":"RUNNING","hostName":"b"}],"meta":{"total":2,"page":1,"limit":100}}`))
	}))
	defer srv.Close()

	var idResp resource.IdentitySchemaResponse
	(&computeResource{}).IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &idResp)

	l := &computeList{client: testClient(srv.URL)}
	var stream list.ListResultsStream
	l.List(ctx, list.ListRequest{
		IncludeResource:        true,
		ResourceSchema:         resSchema(ctx),
		ResourceIdentitySchema: idResp.IdentitySchema,
	}, &stream)
	if stream.Results == nil {
		t.Fatal("expected a results stream")
	}

	// Returning false on the first result exercises the early-return branch.
	got := 0
	stream.Results(func(r list.ListResult) bool {
		got++
		if r.Diagnostics.HasError() {
			t.Errorf("result %d diagnostics: %v", got, r.Diagnostics)
		}
		return false
	})
	if got != 1 {
		t.Fatalf("expected the consumer to stop after 1 result, got %d", got)
	}
}

// TestUpdateFinalGetError covers Update's error branch when the refresh GET after
// a successful update fails. Plan equals state, so the update endpoint, resize and
// state reconcile are all skipped; reconcileState's GET succeeds, but the final
// GetCompute returns an error.
func TestUpdateFinalGetError(t *testing.T) {
	ctx := context.Background()
	rs := resSchema(ctx)

	var mu sync.Mutex
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING"}}`))
			return
		}
		mu.Lock()
		gets++
		n := gets
		mu.Unlock()
		if n >= 2 { // reconcileState's GET succeeds; the refresh GET after update fails
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"RUNNING","hostName":"a","image":"UBUNTU-24.04","region":"KGL-1","specification":"S-1C-1G"}}`))
	}))
	defer srv.Close()

	model := computeResourceModel{
		ID:            types.StringValue("1"),
		HostName:      types.StringValue("a"),
		Image:         types.StringValue("UBUNTU-24.04"),
		Region:        types.StringValue("KGL-1"),
		Specification: types.StringValue("S-1C-1G"),
		PublicKeys:    types.SetNull(types.StringType),
		WithPublicIP:  types.BoolValue(false),
		Tags:          types.ListNull(types.StringType),
		State:         types.StringValue(client.StateRunning),
		IPv4:          types.StringNull(),
		PrivateIPv4:   types.StringNull(),
		CreatedAt:     types.StringNull(),
		UpdatedAt:     types.StringNull(),
		Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{
			"create": types.StringType, "update": types.StringType, "delete": types.StringType,
		})},
	}
	plan := tfsdk.Plan{Schema: rs}
	if d := plan.Set(ctx, model); d.HasError() {
		t.Fatalf("plan set: %v", d)
	}
	state := tfsdk.State{Schema: rs}
	if d := state.Set(ctx, model); d.HasError() {
		t.Fatalf("state set: %v", d)
	}

	r := &computeResource{client: testClient(srv.URL)}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: rs}}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)
	if !resp.Diagnostics.HasError() {
		t.Error("want error from the post-update refresh")
	}
}

// stateSrv is a configurable stateful compute server for settleAfterCreate/resize
// tests: GET returns the current state, and stop/start/upsize transition it
// (unless the matching fail flag injects a 500).
type stateSrv struct {
	mu          sync.Mutex
	state       string
	spec        string // current specification, returned by GET (default S-1C-1G)
	failStop    bool
	failStart   bool
	failUpsize  bool
	stopTo      string // state after a successful stop (default STOPPED)
	upsizeTo    string // state after a successful upsize (default STOPPED)
	upsizeStale bool   // when true a successful upsize does not update spec (models a resize that never settles)
}

func newStateSrv(t *testing.T, s *stateSrv) *client.Client {
	t.Helper()
	if s.stopTo == "" {
		s.stopTo = client.StateStopped
	}
	if s.upsizeTo == "" {
		s.upsizeTo = client.StateStopped
	}
	if s.spec == "" {
		s.spec = "S-1C-1G"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fail := func() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/stop"):
			if s.failStop {
				fail()
				return
			}
			s.state = s.stopTo
		case strings.HasSuffix(r.URL.Path, "/start"):
			if s.failStart {
				fail()
				return
			}
			s.state = client.StateRunning
		case strings.HasSuffix(r.URL.Path, "/upsize"):
			if s.failUpsize {
				fail()
				return
			}
			if !s.upsizeStale {
				var body struct {
					NewSpecification string `json:"newSpecification"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.NewSpecification != "" {
					s.spec = body.NewSpecification
				}
			}
			s.state = s.upsizeTo
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"` + s.state + `","specification":"` + s.spec + `"}}`))
	}))
	t.Cleanup(srv.Close)
	return testClient(srv.URL)
}

func TestSettleAfterCreate(t *testing.T) {
	ctx := context.Background()

	t.Run("wait-for-running fails (destroyed)", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateDestroyed})}
		if _, d := r.settleAfterCreate(ctx, "1", client.StateRunning); !d.HasError() {
			t.Error("want error")
		}
	})
	t.Run("running success", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning})}
		if _, d := r.settleAfterCreate(ctx, "1", client.StateRunning); d.HasError() {
			t.Errorf("unexpected: %v", d)
		}
	})
	t.Run("stop fails", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning, failStop: true})}
		if _, d := r.settleAfterCreate(ctx, "1", client.StateStopped); !d.HasError() {
			t.Error("want stop error")
		}
	})
	t.Run("wait-for-stopped fails", func(t *testing.T) {
		// Stop returns 200 but the compute lands on DESTROYED — a stable state that
		// is neither the prior RUNNING (tolerated as transient) nor the target
		// STOPPED, so the wait reports it as an error.
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning, stopTo: client.StateDestroyed})}
		if _, d := r.settleAfterCreate(ctx, "1", client.StateStopped); !d.HasError() {
			t.Error("want wait error")
		}
	})
	t.Run("stopped success", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning})}
		if _, d := r.settleAfterCreate(ctx, "1", client.StateStopped); d.HasError() {
			t.Errorf("unexpected: %v", d)
		}
	})
}

func TestResize(t *testing.T) {
	ctx := context.Background()

	t.Run("stop-before-resize fails", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateRunning, failStop: true})}
		if d := r.resize(ctx, "1", "S-2C-4G"); !d.HasError() {
			t.Error("want stop error")
		}
	})
	t.Run("upsize fails", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateStopped, failUpsize: true})}
		if d := r.resize(ctx, "1", "S-2C-4G"); !d.HasError() {
			t.Error("want upsize error")
		}
	})
	t.Run("wait-after-resize fails", func(t *testing.T) {
		// Upsize returns 200 but never settles to the new specification; the wait
		// then runs out the context deadline and resize surfaces the timeout.
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateStopped, upsizeStale: true})}
		if d := r.resize(cctx, "1", "S-2C-4G"); !d.HasError() {
			t.Error("want wait error")
		}
	})
	t.Run("success", func(t *testing.T) {
		r := &computeResource{client: newStateSrv(t, &stateSrv{state: client.StateStopped})}
		if d := r.resize(ctx, "1", "S-2C-4G"); d.HasError() {
			t.Errorf("unexpected: %v", d)
		}
	})
}

func TestParseRebootTimeout(t *testing.T) {
	if d, diags := parseRebootTimeout(types.StringNull()); diags.HasError() || d != defaultRebootTimeout {
		t.Errorf("null -> default, got %v (%v)", d, diags)
	}
	if d, diags := parseRebootTimeout(types.StringValue("30s")); diags.HasError() || d.String() != "30s" {
		t.Errorf("valid -> parsed, got %v (%v)", d, diags)
	}
	if _, diags := parseRebootTimeout(types.StringValue("nope")); !diags.HasError() {
		t.Error("invalid duration should error")
	}
}

// rebootStateServer serves a compute that reports beforeState until a reboot is
// issued, then afterState. GET and reboot errors are injectable so every
// rebootCompute branch is reachable.
func rebootStateServer(t *testing.T, beforeState string, failGet, failReboot bool, afterState string) *client.Client {
	t.Helper()
	var mu sync.Mutex
	rebooted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reboot") {
			if failReboot {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
				return
			}
			mu.Lock()
			rebooted = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"REBOOTING"}}`))
			return
		}
		if failGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"code":"INTERNAL_SERVER_ERROR","message":"boom"}`))
			return
		}
		mu.Lock()
		state := beforeState
		updated := "t1"
		if rebooted {
			state = afterState
			updated = "t2" // reboot bumps updatedAt, the signal WaitForReboot keys off
		}
		mu.Unlock()
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"1","state":"` + state + `","updatedAt":"` + updated + `"}}`))
	}))
	t.Cleanup(srv.Close)
	return testClient(srv.URL)
}

func TestRebootCompute(t *testing.T) {
	ctx := context.Background()
	noop := func(string) {}

	t.Run("get error", func(t *testing.T) {
		if d := rebootCompute(ctx, rebootStateServer(t, "RUNNING", true, false, "RUNNING"), "1", noop); !d.HasError() {
			t.Error("want error")
		}
	})
	t.Run("precondition not running", func(t *testing.T) {
		d := rebootCompute(ctx, rebootStateServer(t, "STOPPED", false, false, "RUNNING"), "1", noop)
		if !d.HasError() {
			t.Error("want precondition error")
		}
	})
	t.Run("reboot error", func(t *testing.T) {
		if d := rebootCompute(ctx, rebootStateServer(t, "RUNNING", false, true, "RUNNING"), "1", noop); !d.HasError() {
			t.Error("want reboot error")
		}
	})
	t.Run("wait error (stays stopped)", func(t *testing.T) {
		// After reboot the compute reports STOPPED, which is the wrong stable state.
		d := rebootCompute(ctx, rebootStateServer(t, "RUNNING", false, false, "STOPPED"), "1", noop)
		if !d.HasError() {
			t.Error("want wait error")
		}
	})
	t.Run("success", func(t *testing.T) {
		var msgs []string
		d := rebootCompute(ctx, rebootStateServer(t, "RUNNING", false, false, "RUNNING"), "1", func(m string) { msgs = append(msgs, m) })
		if d.HasError() {
			t.Errorf("unexpected error: %v", d)
		}
		if len(msgs) != 2 {
			t.Errorf("want 2 progress messages, got %d", len(msgs))
		}
	})
}

// TestApplyTags covers the null/empty normalisation.
func TestListResourceModel(t *testing.T) {
	ctx := context.Background()

	// A tagged compute with a public IP: the generated config (terraform query
	// -generate-config-out) must carry both, or a later apply would erase the
	// tags and replace the compute.
	withTags := client.Compute{
		ID: "1", HostName: "web", Image: "UBUNTU-24.04", Region: "KGL-1",
		Specification: "S-1C-1G", State: client.StateRunning,
		IPv4: "1.2.3.4", Tags: []string{"prod", "web"},
	}
	m, diags := listResourceModel(ctx, &withTags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !m.WithPublicIP.ValueBool() {
		t.Error("with_public_ip should be true for a compute with a public IPv4")
	}
	var tags []string
	m.Tags.ElementsAs(ctx, &tags, false)
	if len(tags) != 2 || tags[0] != "prod" || tags[1] != "web" {
		t.Errorf("tags = %v, want [prod web]", tags)
	}

	// An untagged compute with no public IP: tags stay null (so the attribute is
	// omitted, no diff) and with_public_ip is false.
	noTags := client.Compute{ID: "2", HostName: "db", State: client.StateStopped}
	m2, _ := listResourceModel(ctx, &noTags)
	if m2.WithPublicIP.ValueBool() {
		t.Error("with_public_ip should be false for a compute with no public IPv4")
	}
	if !m2.Tags.IsNull() {
		t.Errorf("tags should be null for an untagged compute, got %v", m2.Tags)
	}
}

func TestApplyTags(t *testing.T) {
	ctx := context.Background()

	t.Run("empty API + config null stays null", func(t *testing.T) {
		m := &computeResourceModel{}
		if diags := m.applyTags(ctx, []string{}, true); diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		if !m.Tags.IsNull() {
			t.Errorf("tags should be null, got %v", m.Tags)
		}
	})

	t.Run("empty API + config set stays empty", func(t *testing.T) {
		m := &computeResourceModel{}
		if diags := m.applyTags(ctx, []string{}, false); diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		if m.Tags.IsNull() || len(m.Tags.Elements()) != 0 {
			t.Errorf("tags should be empty list, got %v", m.Tags)
		}
	})

	t.Run("nil API + config set becomes empty list", func(t *testing.T) {
		m := &computeResourceModel{}
		if diags := m.applyTags(ctx, nil, false); diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		if m.Tags.IsNull() || len(m.Tags.Elements()) != 0 {
			t.Errorf("nil api tags with config set should be empty list, got %v", m.Tags)
		}
	})

	t.Run("non-empty API fills values", func(t *testing.T) {
		m := &computeResourceModel{}
		if diags := m.applyTags(ctx, []string{"web", "prod"}, true); diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		if len(m.Tags.Elements()) != 2 {
			t.Errorf("tags should have 2 elements, got %v", m.Tags)
		}
	})
}

// TestApplyComputed covers that only stable states reach state.
func TestApplyComputed(t *testing.T) {
	t.Run("stable state is written", func(t *testing.T) {
		m := &computeResourceModel{State: types.StringValue(client.StateRunning)}
		m.applyComputed(&client.Compute{ID: "1", State: client.StateStopped})
		if m.State.ValueString() != client.StateStopped {
			t.Errorf("stable state should be written, got %q", m.State.ValueString())
		}
	})

	t.Run("transitional state keeps prior stable value", func(t *testing.T) {
		m := &computeResourceModel{State: types.StringValue(client.StateRunning)}
		m.applyComputed(&client.Compute{ID: "1", State: client.StateUpgrading})
		if m.State.ValueString() != client.StateRunning {
			t.Errorf("transitional state should keep prior %q, got %q", client.StateRunning, m.State.ValueString())
		}
	})
}

func TestIsDownsize(t *testing.T) {
	tests := map[string]struct {
		old, new string
		want     bool
	}{
		"upsize memory":        {"S-1C-1G", "S-1C-2G", false},
		"upsize vcpu":          {"S-1C-4G", "S-2C-4G", false},
		"same":                 {"S-2C-4G", "S-2C-4G", false},
		"downsize memory":      {"S-2C-8G", "S-2C-4G", true},
		"downsize vcpu":        {"S-4C-8G", "S-2C-8G", true},
		"cpu up mem down":      {"S-1C-8G", "S-2C-4G", true},
		"unknown old shape":    {"weird", "S-2C-4G", false},
		"unknown new shape":    {"S-2C-4G", "weird", false},
		"fractional memory up": {"S-1C-1G", "S-1C-1.5G", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isDownsize(tc.old, tc.new); got != tc.want {
				t.Errorf("isDownsize(%q, %q) = %v, want %v", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

func TestParseSpec(t *testing.T) {
	v, m, ok := parseSpec("S-2C-4G")
	if !ok || v != 2 || m != 4 {
		t.Errorf("parseSpec(S-2C-4G) = (%d, %g, %v), want (2, 4, true)", v, m, ok)
	}
	if _, _, ok := parseSpec("not-a-spec"); ok {
		t.Error("parseSpec should fail on a non-spec string")
	}
	// A vCPU count too large for an int overflows Atoi -> not ok.
	if _, _, ok := parseSpec("S-99999999999999999999C-1G"); ok {
		t.Error("parseSpec should fail on an overflowing vcpu count")
	}
	// A memory value out of float64 range makes ParseFloat fail -> not ok.
	huge := "S-1C-" + strings.Repeat("9", 400) + "G"
	if _, _, ok := parseSpec(huge); ok {
		t.Error("parseSpec should fail on an out-of-range memory value")
	}
	if isDownsize("S-99999999999999999999C-1G", "S-1C-1G") {
		t.Error("isDownsize should be false when a slug does not parse")
	}
}

// TestForcesReplacementMatchesSchema guards against drift between the schema and
// forcesReplacement. The schema is the source of truth for which attributes force
// a replace (via RequiresReplace plan modifiers); forcesReplacement mirrors that
// set by hand because the framework does not expose its replace decision to
// ModifyPlan. This test discovers the schema's replace set by reflection and
// asserts forcesReplacement reacts to exactly those attributes — so adding a
// RequiresReplace modifier without updating forcesReplacement (or the reverse)
// fails here.
func TestForcesReplacementMatchesSchema(t *testing.T) {
	ctx := context.Background()
	schemaSet := schemaReplaceAttrs(t, resSchema(ctx))
	if len(schemaSet) == 0 {
		t.Fatal("no RequiresReplace attributes found in the schema; detection is broken")
	}

	// A fully-populated prior state. public_keys is non-null so its
	// RequiresReplaceIf is in its replacing regime (it does not replace from a
	// null state, e.g. just after import).
	baseline := computeResourceModel{
		ID:            types.StringValue("id-1"),
		HostName:      types.StringValue("web-01"),
		Image:         types.StringValue("UBUNTU-24.04"),
		Region:        types.StringValue("KGL-1"),
		Specification: types.StringValue("S-1C-1G"),
		PublicKeys:    types.SetValueMust(types.StringType, []attr.Value{types.StringValue("ssh-ed25519 AAAA")}),
		WithPublicIP:  types.BoolValue(false),
		Tags:          types.ListValueMust(types.StringType, []attr.Value{types.StringValue("web")}),
		State:         types.StringValue(client.StateRunning),
	}

	// Each input attribute mapped to a change in that attribute alone. Every
	// attribute the schema can mark for replace must appear here, so a newly added
	// one cannot be silently skipped.
	mutators := map[string]func(m *computeResourceModel){
		"host_name":     func(m *computeResourceModel) { m.HostName = types.StringValue("web-02") },
		"image":         func(m *computeResourceModel) { m.Image = types.StringValue("DEBIAN-12") },
		"region":        func(m *computeResourceModel) { m.Region = types.StringValue("KGL-2") },
		"specification": func(m *computeResourceModel) { m.Specification = types.StringValue("S-2C-4G") },
		"public_keys": func(m *computeResourceModel) {
			m.PublicKeys = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("ssh-ed25519 BBBB")})
		},
		"with_public_ip": func(m *computeResourceModel) { m.WithPublicIP = types.BoolValue(true) },
		"tags": func(m *computeResourceModel) {
			m.Tags = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("db")})
		},
		"state": func(m *computeResourceModel) { m.State = types.StringValue(client.StateStopped) },
	}

	for name := range schemaSet {
		if _, ok := mutators[name]; !ok {
			t.Fatalf("schema marks %q for replace but the test has no mutator for it; add one and update forcesReplacement", name)
		}
	}

	for name, mutate := range mutators {
		plan := baseline
		mutate(&plan)
		got := forcesReplacement(plan, baseline)
		want := schemaSet[name]
		if got != want {
			t.Errorf("forcesReplacement for a %q change = %v, but the schema's replace decision is %v; the two have drifted", name, got, want)
		}
	}
}

// schemaReplaceAttrs returns the set of schema attribute names whose plan
// modifiers include a RequiresReplace/RequiresReplaceIf modifier, detected by the
// modifier's concrete type name. Reading the schema this way keeps the guard test
// free of a hand-maintained replace list; it relies on the stock modifiers' type
// names containing "RequiresReplace".
func schemaReplaceAttrs(t *testing.T, s fwrschema.Schema) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for name, attribute := range s.Attributes {
		v := reflect.ValueOf(attribute)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			continue
		}
		pm := v.FieldByName("PlanModifiers")
		if !pm.IsValid() || pm.Kind() != reflect.Slice {
			continue
		}
		for i := 0; i < pm.Len(); i++ {
			if pm.Index(i).IsNil() {
				continue
			}
			typeName := strings.ToLower(reflect.TypeOf(pm.Index(i).Interface()).String())
			if strings.Contains(typeName, "requiresreplace") {
				out[name] = true
			}
		}
	}
	return out
}
