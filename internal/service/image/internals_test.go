package image

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

func catalog() []client.Image {
	return []client.Image{
		{Image: "UBUNTU-24.04", ImageName: "Ubuntu", Version: "24.04", DisplayName: "Ubuntu 24.04 LTS"},
		{Image: "UBUNTU-22.04", ImageName: "Ubuntu", Version: "22.04", DisplayName: "Ubuntu 22.04 LTS"},
		{Image: "DEBIAN-13", ImageName: "Debian", Version: "13", DisplayName: "Debian 13"},
	}
}

func TestMatchImages(t *testing.T) {
	tests := map[string]struct {
		cfg  imageModel
		want int
	}{
		"by slug: one":         {imageModel{Image: types.StringValue("DEBIAN-13")}, 1},
		"by name: many":        {imageModel{ImageName: types.StringValue("Ubuntu")}, 2},
		"by name+version: one": {imageModel{ImageName: types.StringValue("Ubuntu"), Version: types.StringValue("22.04")}, 1},
		"no match":             {imageModel{Image: types.StringValue("NOPE")}, 0},
		"all null: all":        {imageModel{}, 3},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := normalizeNulls(tc.cfg)
			got := matchImages(catalog(), cfg)
			if len(got) != tc.want {
				t.Errorf("got %d matches, want %d", len(got), tc.want)
			}
		})
	}
}

func normalizeNulls(m imageModel) imageModel {
	if m.Image.ValueString() == "" {
		m.Image = types.StringNull()
	}
	if m.ImageName.ValueString() == "" {
		m.ImageName = types.StringNull()
	}
	if m.Version.ValueString() == "" {
		m.Version = types.StringNull()
	}
	return m
}

// TestReadGuard covers the diagnostics guard after Config.Get by feeding a
// malformed configuration value.
func TestReadGuard(t *testing.T) {
	ctx := context.Background()
	d := &imageDataSource{}
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
