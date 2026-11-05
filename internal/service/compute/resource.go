package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
	scvalidators "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/validators"
)

// Default timeouts.
const (
	defaultCreateTimeout = 15 * time.Minute
	defaultUpdateTimeout = 20 * time.Minute
	defaultDeleteTimeout = 10 * time.Minute
)

// Interface assertions.
var (
	_ resource.Resource                = &computeResource{}
	_ resource.ResourceWithConfigure   = &computeResource{}
	_ resource.ResourceWithImportState = &computeResource{}
	_ resource.ResourceWithModifyPlan  = &computeResource{}
	_ resource.ResourceWithIdentity    = &computeResource{}
)

// NewResource returns the strettchcloud_compute resource.
func NewResource() resource.Resource {
	return &computeResource{}
}

type computeResource struct {
	client *client.Client
}

func (r *computeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute"
}

func (r *computeResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Strettch Cloud compute instance.",
		Attributes: map[string]schema.Attribute{
			// --- Inputs (customer writes) ---
			"host_name": schema.StringAttribute{
				MarkdownDescription: "Host name. Updated in place. Must be non-empty with no leading or trailing whitespace.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					scvalidators.NoSurroundingWhitespace(),
				},
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Image slug, e.g. `UBUNTU-24.04`. Changing it replaces the compute. Catalog validity is checked by the API.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					scvalidators.NoSurroundingWhitespace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region slug, e.g. `KGL-1`. Changing it replaces the compute. Catalog validity is checked by the API.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					scvalidators.NoSurroundingWhitespace(),
				},
			},
			"specification": schema.StringAttribute{
				MarkdownDescription: "Specification slug, e.g. `S-1C-1G`. Raising it resizes the compute in place (upward only; downsizing is an error). The compute is stopped during the resize.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					scvalidators.NoSurroundingWhitespace(),
				},
			},
			"public_keys": schema.SetAttribute{
				MarkdownDescription: "SSH public keys. Cannot change after creation (changing them replaces the compute). Not recoverable on import; the first apply after import writes them in place without a replace.",
				Required:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					// Replace when public_keys change, except right after import
					// where the prior state is null.
					setplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.SetRequest, resp *setplanmodifier.RequiresReplaceIfFuncResponse) {
							resp.RequiresReplace = !req.StateValue.IsNull()
						},
						"Replace when public_keys change, unless imported (prior state null).",
						"Replace when `public_keys` change, unless imported (prior state null).",
					),
				},
				// No key-format validation, the public key is passed through and the API is the
				// source of truth for its validity. SizeAtLeast(1) only requires
				// at least one key.
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
				},
			},
			"with_public_ip": schema.BoolAttribute{
				MarkdownDescription: "Whether to attach a public IP. Cannot change after creation (changing it replaces the compute). Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags. Updated in place. Each tag must be non-empty with no leading or trailing whitespace.",
				Optional:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(
						stringvalidator.LengthAtLeast(1),
						scvalidators.NoSurroundingWhitespace(),
					),
				},
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Desired power state: `RUNNING` or `STOPPED`. Updated in place via start/stop. Defaults to `RUNNING`. Not sent on create; every new compute starts.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(client.StateRunning),
				Validators: []validator.String{
					stringvalidator.OneOf(client.StateRunning, client.StateStopped),
				},
			},

			// --- Outputs (computed; filled from the API response) ---
			"id": schema.StringAttribute{
				MarkdownDescription: "Compute id.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ipv4": schema.StringAttribute{
				MarkdownDescription: "Public IPv4 address.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"private_ipv4": schema.StringAttribute{
				MarkdownDescription: "Private IPv4 address.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last update timestamp.",
				Computed:            true,
			},

			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *computeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = svcutil.ResourceClient(req, resp)
}

// Create provisions a compute, saves its id immediately, then polls to the
// configured stable state.
func (r *computeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan computeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := svcutil.WithTimeout(ctx, plan.Timeouts.Create, defaultCreateTimeout, &resp.Diagnostics)
	defer cancel()

	createReq, diags := plan.toCreateRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateCompute(ctx, createReq, uuid.NewString())
	if err != nil {
		resp.Diagnostics.AddError("Creating compute failed", err.Error())
		return
	}

	// Save the id at once so a later failure taints rather than orphans.
	// The other computed outputs are set to null (known values) so a failure
	// before the final write leaves no unknown values in state.
	plan.ID = types.StringValue(created.ID)
	plan.IPv4 = types.StringNull()
	plan.PrivateIPv4 = types.StringNull()
	plan.CreatedAt = types.StringNull()
	plan.UpdatedAt = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Poll to the configured stable state (RUNNING, or STOPPED if requested).
	final, diags := r.settleAfterCreate(ctx, created.ID, plan.State.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.writeState(ctx, &resp.State, &resp.Diagnostics, plan, final)
	setIdentity(ctx, resp.Identity, &resp.Diagnostics, created.ID)
}

// settleAfterCreate polls a freshly created compute to RUNNING, then stops it if the
// configured state is STOPPED. Split from Create so every poll/stop branch is
// unit-testable with a mock client.
func (r *computeResource) settleAfterCreate(ctx context.Context, id, targetState string) (*client.Compute, diag.Diagnostics) {
	var diags diag.Diagnostics

	final, err := r.client.WaitForRunning(ctx, id)
	if err != nil {
		diags.AddError("Compute did not reach RUNNING", err.Error())
		return nil, diags
	}

	if targetState == client.StateStopped {
		if _, err := r.client.StopCompute(ctx, id); err != nil {
			diags.AddError("Stopping compute failed", err.Error())
			return nil, diags
		}
		final, err = r.client.WaitForState(ctx, id, client.StateRunning, client.StateStopped)
		if err != nil {
			diags.AddError("Compute did not reach STOPPED", err.Error())
			return nil, diags
		}
	}
	return final, diags
}

// Read refreshes state from the API. A 404 or DESTROYED removes the resource
// from state without error.
func (r *computeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state computeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := r.client.GetCompute(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading compute failed", err.Error())
		return
	}
	if c.State == client.StateDestroyed {
		resp.State.RemoveResource(ctx)
		return
	}

	r.writeState(ctx, &resp.State, &resp.Diagnostics, state, c)
	setIdentity(ctx, resp.Identity, &resp.Diagnostics, c.ID)
}

// Update applies, in order: host_name/tags in place, specification via resize,
// then state via start/stop. public_keys after import is written to state only.
func (r *computeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state computeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	ctx, cancel := svcutil.WithTimeout(ctx, plan.Timeouts.Update, defaultUpdateTimeout, &resp.Diagnostics)
	defer cancel()

	// 1. host_name / tags in place via the update endpoint.
	if hostNameOrTagsChanged(plan, state) {
		updReq, diags := buildUpdateRequest(ctx, plan, state)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if _, err := r.client.UpdateCompute(ctx, id, updReq); err != nil {
			resp.Diagnostics.AddError("Updating compute failed", err.Error())
			return
		}
	}

	// 2. specification: resize up (a downsize is rejected in ModifyPlan). The API
	// requires a STOPPED compute to upsize, so stop it first and wait; the resize
	// leaves it STOPPED, and step 3 restarts it if the configured state is
	// RUNNING.
	if !plan.Specification.Equal(state.Specification) {
		resp.Diagnostics.Append(r.resize(ctx, id, plan.Specification.ValueString())...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// 3. state: start/stop to the configured target.
	target := plan.State.ValueString()
	if err := r.reconcileState(ctx, id, target); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Compute did not reach %s", target), err.Error())
		return
	}

	final, err := r.client.GetCompute(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading compute after update failed", err.Error())
		return
	}
	r.writeState(ctx, &resp.State, &resp.Diagnostics, plan, final)
}

// resize stops the compute, upsizes it, and waits for it to settle STOPPED. The
// API requires a STOPPED compute to upsize. Split from Update so every branch is
// unit-testable.
func (r *computeResource) resize(ctx context.Context, id, newSpecification string) diag.Diagnostics {
	var diags diag.Diagnostics

	if err := r.reconcileState(ctx, id, client.StateStopped); err != nil {
		diags.AddError("Stopping compute before resize failed", err.Error())
		return diags
	}
	if _, err := r.client.UpsizeCompute(ctx, id, newSpecification); err != nil {
		diags.AddError("Resizing compute failed", err.Error())
		return diags
	}
	if _, err := r.client.WaitForSpecification(ctx, id, newSpecification); err != nil {
		diags.AddError("Compute did not finish resizing", err.Error())
		return diags
	}
	return diags
}

// reconcileState drives the compute to target (RUNNING or STOPPED) via start or
// stop, then polls. It is a no-op if the compute is already there.
func (r *computeResource) reconcileState(ctx context.Context, id, target string) error {
	current, err := r.client.GetCompute(ctx, id)
	if err != nil {
		return err
	}
	if current.State == target {
		return nil
	}

	switch target {
	case client.StateRunning:
		if _, err := r.client.StartCompute(ctx, id); err != nil {
			return err
		}
	case client.StateStopped:
		if _, err := r.client.StopCompute(ctx, id); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported target state %q", target)
	}

	_, err = r.client.WaitForState(ctx, id, current.State, target)
	return err
}

// hostNameOrTagsChanged reports whether either field needs the update endpoint.
func hostNameOrTagsChanged(plan, state computeResourceModel) bool {
	return !plan.HostName.Equal(state.HostName) || !plan.Tags.Equal(state.Tags)
}

// buildUpdateRequest builds the PATCH body for whichever of host_name/tags
// changed.
func buildUpdateRequest(ctx context.Context, plan, state computeResourceModel) (client.ComputeUpdateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	var req client.ComputeUpdateRequest

	if !plan.HostName.Equal(state.HostName) {
		v := plan.HostName.ValueString()
		req.HostName = &v
	}
	if !plan.Tags.Equal(state.Tags) {
		tags, d := stringSlice(ctx, plan.Tags)
		diags.Append(d...)
		if tags == nil {
			tags = []string{}
		}
		req.Tags = &tags
	}
	return req, diags
}

// Delete removes the compute and polls until it is gone.
func (r *computeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state computeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := svcutil.WithTimeout(ctx, state.Timeouts.Delete, defaultDeleteTimeout, &resp.Diagnostics)
	defer cancel()

	id := state.ID.ValueString()
	if err := r.client.DeleteCompute(ctx, id); err != nil {
		resp.Diagnostics.AddError("Deleting compute failed", err.Error())
		return
	}
	if _, err := r.client.WaitForState(ctx, id, state.State.ValueString(), client.StateDestroyed); err != nil {
		resp.Diagnostics.AddError("Compute was not destroyed", err.Error())
		return
	}
}

// ImportState imports by id or by identity. Read fills what the API returns;
// public_keys stays empty until the next apply.
func (r *computeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("id"), path.Root("id"), req, resp)
}

// Resource identity. The identity is just the id and never changes; it is set
// on every Create, Read and Import, and the list resource returns it for each
// compute.
type computeIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

func (r *computeResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "Compute id.",
			},
		},
	}
}

// setIdentity writes the resource identity (just the id). It is a no-op when the
// response carries no identity (older Terraform without identity support).
func setIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, diags *diag.Diagnostics, id string) {
	if identity == nil {
		return
	}
	diags.Append(identity.Set(ctx, computeIdentityModel{ID: types.StringValue(id)})...)
}

// writeState merges the fresh API response into the model and saves it to
// Terraform state, applying the drift rules: computed fields from the API;
// public_keys/with_public_ip kept from prior; tags normalised.
func (r *computeResource) writeState(ctx context.Context, state *tfsdk.State, diags *diag.Diagnostics, prior computeResourceModel, c *client.Compute) {
	configTagsNull := prior.Tags.IsNull()
	prior.applyComputed(c)
	diags.Append(prior.applyTags(ctx, c.Tags, configTagsNull)...)
	// with_public_ip is not returned; on a fresh import derive it from ipv4.
	if prior.WithPublicIP.IsNull() || prior.WithPublicIP.IsUnknown() {
		prior.WithPublicIP = types.BoolValue(c.IPv4 != "")
	}
	diags.Append(state.Set(ctx, prior)...)
}
