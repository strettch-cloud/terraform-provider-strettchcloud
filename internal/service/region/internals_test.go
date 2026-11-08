package region

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestReadGuard covers the diagnostics guard after Config.Get by feeding a
// malformed configuration value.
func TestReadGuard(t *testing.T) {
	ctx := context.Background()
	d := &regionDataSource{}
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema}}
	d.Read(ctx, datasource.ReadRequest{
		Config: tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(tftypes.String, "nope")},
	}, resp)
	if !resp.Diagnostics.HasError() {
		t.Error("malformed config should produce an error")
	}
}
