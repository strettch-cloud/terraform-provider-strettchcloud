// Package validators holds the custom string validators the schema uses beyond
// the stock framework-validators: a no-surrounding-whitespace guard (the API
// trims, so leading/trailing whitespace would cause a perpetual diff).
package validators

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NoSurroundingWhitespace rejects a string with leading or trailing whitespace.
// Inner spaces are allowed.
func NoSurroundingWhitespace() validator.String {
	return noWhitespaceValidator{}
}

type noWhitespaceValidator struct{}

func (v noWhitespaceValidator) Description(_ context.Context) string {
	return "must not have leading or trailing whitespace"
}

func (v noWhitespaceValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noWhitespaceValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if s != strings.TrimSpace(s) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid value",
			"Value must not have leading or trailing whitespace; the API trims these, which would cause a perpetual diff.",
		)
	}
}
