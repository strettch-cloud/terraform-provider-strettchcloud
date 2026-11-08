package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func runString(v validator.String, val types.String) bool {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{
		Path:        path.Root("field"),
		ConfigValue: val,
	}, resp)
	return resp.Diagnostics.HasError()
}

func TestValidatorDescriptions(t *testing.T) {
	ctx := context.Background()
	for _, v := range []interface {
		Description(context.Context) string
		MarkdownDescription(context.Context) string
	}{NoSurroundingWhitespace()} {
		if v.Description(ctx) == "" || v.MarkdownDescription(ctx) == "" {
			t.Error("validator descriptions must be non-empty")
		}
	}
}

func TestNoSurroundingWhitespace(t *testing.T) {
	v := NoSurroundingWhitespace()
	cases := map[string]struct {
		in      types.String
		wantErr bool
	}{
		"clean":       {types.StringValue("web-01"), false},
		"inner space": {types.StringValue("my server"), false},
		"leading":     {types.StringValue(" web"), true},
		"trailing":    {types.StringValue("web "), true},
		"null":        {types.StringNull(), false},
		"unknown":     {types.StringUnknown(), false},
	}
	for name, tc := range cases {
		if got := runString(v, tc.in); got != tc.wantErr {
			t.Errorf("%s: err=%v want %v", name, got, tc.wantErr)
		}
	}
}
