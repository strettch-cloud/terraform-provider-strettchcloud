package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/svcutil"
)

// Reboot action (Terraform Actions, v1.1.0). Reboot is a verb, not a state, so
// it is a one-off action that leaves nothing in state. It reboots a RUNNING
// compute and polls it back to RUNNING; the API rejects reboot on a stopped
// compute, so the action checks first and returns a clear error.
var (
	_ action.Action              = &computeRebootAction{}
	_ action.ActionWithConfigure = &computeRebootAction{}
)

const defaultRebootTimeout = 15 * time.Minute

// NewRebootAction returns the strettchcloud_compute_reboot action.
func NewRebootAction() action.Action {
	return &computeRebootAction{}
}

type computeRebootAction struct {
	client *client.Client
}

type rebootActionModel struct {
	ComputeID types.String `tfsdk:"compute_id"`
	Timeout   types.String `tfsdk:"timeout"`
}

func (a *computeRebootAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_reboot"
}

func (a *computeRebootAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reboots a running compute instance. The compute must be `RUNNING`; rebooting a stopped compute is an error.",
		Attributes: map[string]schema.Attribute{
			"compute_id": schema.StringAttribute{
				MarkdownDescription: "Id of the compute to reboot.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for the reboot to complete, as a Go duration (e.g. `15m`). Defaults to `15m`.",
				Optional:            true,
			},
		},
	}
}

func (a *computeRebootAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = svcutil.ActionClient(req, resp)
}

func (a *computeRebootAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var data rebootActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := parseRebootTimeout(data.Timeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp.Diagnostics.Append(rebootCompute(ctx, a.client, data.ComputeID.ValueString(), func(msg string) {
		resp.SendProgress(action.InvokeProgressEvent{Message: msg})
	})...)
}

// parseRebootTimeout reads the optional timeout config, returning the default
// when unset and a diagnostic when it is not a valid duration.
func parseRebootTimeout(v types.String) (time.Duration, diag.Diagnostics) {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() {
		return defaultRebootTimeout, diags
	}
	parsed, err := time.ParseDuration(v.ValueString())
	if err != nil {
		diags.AddError("Invalid timeout", fmt.Sprintf("Could not parse timeout %q as a duration: %s", v.ValueString(), err))
		return defaultRebootTimeout, diags
	}
	return parsed, diags
}

// rebootCompute is the reboot orchestration, split out from Invoke so every
// branch is unit-testable with a mock client: it enforces the RUNNING
// precondition, issues the reboot, and polls back to RUNNING, reporting progress
// through the given callback.
func rebootCompute(ctx context.Context, c *client.Client, id string, progress func(string)) diag.Diagnostics {
	var diags diag.Diagnostics

	current, err := c.GetCompute(ctx, id)
	if err != nil {
		diags.AddError("Rebooting compute failed", err.Error())
		return diags
	}
	if current.State != client.StateRunning {
		diags.AddError(
			"Compute is not running",
			fmt.Sprintf("Compute %s is %s; reboot requires a RUNNING compute.", id, current.State),
		)
		return diags
	}

	progress(fmt.Sprintf("rebooting compute %s", id))

	if _, err := c.RebootCompute(ctx, id); err != nil {
		diags.AddError("Rebooting compute failed", err.Error())
		return diags
	}

	// The pre-reboot state is RUNNING too, so WaitForReboot uses updatedAt to tell
	// the post-reboot RUNNING from the one we started with.
	if _, err := c.WaitForReboot(ctx, id, current.UpdatedAt); err != nil {
		diags.AddError("Compute did not return to RUNNING after reboot", err.Error())
		return diags
	}

	progress(fmt.Sprintf("compute %s is running", id))
	return diags
}
