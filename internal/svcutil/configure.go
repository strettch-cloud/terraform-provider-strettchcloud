// Package svcutil holds small helpers the service packages share, instead of
// duplicating them: pulling the configured *client.Client out of the Framework's
// ProviderData, and deriving a timeout-bounded context. It imports only the
// Framework and internal/client, so it adds no import cycle.
package svcutil

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

// DataSourceClient extracts the *client.Client a data source's Configure
// receives through ProviderData. It returns nil (without error) when
// ProviderData is nil, which happens on the first Configure pass before the
// provider is configured — the Framework calls Configure again later.
func DataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return nil
	}
	return c
}

// ResourceClient is the resource-side equivalent of DataSourceClient. It also
// serves list resources, whose Configure uses the same resource.ConfigureRequest.
func ResourceClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return nil
	}
	return c
}

// ActionClient is the action-side equivalent of DataSourceClient.
func ActionClient(req action.ConfigureRequest, resp *action.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return nil
	}
	return c
}

// TimeoutReader is the signature of timeouts.Value's Create/Update/Delete
// methods: given a default, it returns the effective timeout and any diagnostics.
type TimeoutReader = func(context.Context, time.Duration) (time.Duration, diag.Diagnostics)

// WithTimeout derives a context bounded by the timeout the reader returns (or def
// when unset), appending any diagnostics. The returned cancel must be deferred by
// the caller.
func WithTimeout(ctx context.Context, read TimeoutReader, def time.Duration, diags *diag.Diagnostics) (context.Context, context.CancelFunc) {
	d, td := read(ctx, def)
	diags.Append(td...)
	return context.WithTimeout(ctx, d)
}
