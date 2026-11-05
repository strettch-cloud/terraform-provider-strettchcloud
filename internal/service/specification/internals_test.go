package specification

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

func catalog() []client.Specification {
	return []client.Specification{
		{ID: "a", Specification: "S-1C-1G", VCPUs: 1, MemoryMB: 1024, StorageGB: 20},
		{ID: "b", Specification: "S-2C-4G", VCPUs: 2, MemoryMB: 4096, StorageGB: 40},
		{ID: "c", Specification: "S-2C-8G", VCPUs: 2, MemoryMB: 8192, StorageGB: 80},
	}
}

func TestMatchSpecifications(t *testing.T) {
	tests := map[string]struct {
		cfg  specificationModel
		want int
	}{
		"by slug: one": {
			cfg:  specificationModel{Specification: types.StringValue("S-2C-4G")},
			want: 1,
		},
		"by vcpus: many": {
			cfg:  specificationModel{VCPUs: types.Int64Value(2)},
			want: 2,
		},
		"by vcpus+memory: one": {
			cfg:  specificationModel{VCPUs: types.Int64Value(2), MemoryMB: types.Float64Value(8192)},
			want: 1,
		},
		"no match": {
			cfg:  specificationModel{VCPUs: types.Int64Value(99)},
			want: 0,
		},
		"all null: all": {
			cfg:  specificationModel{},
			want: 3,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// A null-value model needs its fields explicitly null, not zero value.
			cfg := normalizeNulls(tc.cfg)
			got := matchSpecifications(catalog(), cfg)
			if len(got) != tc.want {
				t.Errorf("got %d matches, want %d", len(got), tc.want)
			}
		})
	}
}

// normalizeNulls turns Go zero-value types.* fields (which are not "null") into
// explicit nulls so the match filters skip them.
func normalizeNulls(m specificationModel) specificationModel {
	if m.Specification.IsNull() || m.Specification.ValueString() == "" {
		m.Specification = types.StringNull()
	}
	if m.VCPUs.ValueInt64() == 0 {
		m.VCPUs = types.Int64Null()
	}
	if m.MemoryMB.ValueFloat64() == 0 {
		m.MemoryMB = types.Float64Null()
	}
	return m
}

// TestReadGuard covers the diagnostics guard after Config.Get by feeding a
// malformed configuration value.
func TestReadGuard(t *testing.T) {
	ctx := context.Background()
	d := &specificationDataSource{}
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
