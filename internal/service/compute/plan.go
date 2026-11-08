package compute

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

func specificationPath() path.Path { return path.Root("specification") }

// specSlug matches a specification slug like "S-2C-4G": vCPU count and memory in
// GB. Catalog validity is the API's job; this is only used to compare sizes.
var specSlug = regexp.MustCompile(`^S-(\d+)C-(\d+(?:\.\d+)?)G$`)

// parseSpec extracts (vcpus, memoryGB) from a slug. ok is false when the slug is
// not in the expected shape, in which case callers do not block on size.
func parseSpec(slug string) (vcpus int, memGB float64, ok bool) {
	m := specSlug.FindStringSubmatch(slug)
	if m == nil {
		return 0, 0, false
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	mem, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return 0, 0, false
	}
	return v, mem, true
}

// isDownsize reports whether moving from oldSlug to newSlug shrinks either
// dimension (vCPU or memory).
func isDownsize(oldSlug, newSlug string) bool {
	oldV, oldM, ok1 := parseSpec(oldSlug)
	newV, newM, ok2 := parseSpec(newSlug)
	if !ok1 || !ok2 {
		return false
	}
	return newV < oldV || newM < oldM
}

// forcesReplacement reports whether any field that triggers a resource
// replacement — image, region, public_keys, or with_public_ip — changed. These
// mirror the schema's RequiresReplace modifiers because ModifyPlan cannot read
// the framework's own replace decision;
func forcesReplacement(plan, state computeResourceModel) bool {
	// public_keys replaces only from a non-null prior; after import it is null, so
	// setting it does not replace (matches the schema's RequiresReplaceIf).
	publicKeysReplaced := !state.PublicKeys.IsNull() && !plan.PublicKeys.Equal(state.PublicKeys)
	return !plan.Image.Equal(state.Image) ||
		!plan.Region.Equal(state.Region) ||
		publicKeysReplaced ||
		!plan.WithPublicIP.Equal(state.WithPublicIP)
}

// ModifyPlan validates catalog slugs at plan time, rejects a downsize, and warns
// about the resize outage. Replacement decisions live in the schema's plan
// modifiers.
func (r *computeResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Nothing to do on destroy (no plan).
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan computeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// state is nil on create; present on update.
	var state *computeResourceModel
	if !req.State.Raw.IsNull() {
		var s computeResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
		if resp.Diagnostics.HasError() {
			return
		}
		state = &s
	}

	// Validate image / region / specification against the catalog at plan time, so
	// a bad slug fails here with a clear message instead of a 400 mid-apply.
	r.validateCatalog(ctx, plan, state, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	r.validateSpecificationChange(plan, state, resp)
}

// validateSpecificationChange handles an update that changes the compute spec:
// validateSpecificationChange rejects unsupported downsizes and warns about
// resize outages. It skips creates, unchanged specs, and updates already
// requiring replacement, since the compute is recreated at the planned spec.
func (r *computeResource) validateSpecificationChange(plan computeResourceModel, state *computeResourceModel, resp *resource.ModifyPlanResponse) {
	if state == nil || plan.Specification.Equal(state.Specification) {
		return
	}
	if forcesReplacement(plan, *state) {
		return
	}

	// Downsizing is not supported; the API only upsizes.
	if isDownsize(state.Specification.ValueString(), plan.Specification.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			specificationPath(),
			"Downsizing is not supported",
			fmt.Sprintf("Cannot change specification from %s to a smaller %s; the API only resizes upward. Replace the compute if you need a smaller size.",
				state.Specification.ValueString(), plan.Specification.ValueString()),
		)
		return
	}

	// Warn that a running compute is stopped during the resize.
	if plan.State.ValueString() == client.StateRunning {
		resp.Diagnostics.AddWarning(
			"Compute will be stopped during resize",
			fmt.Sprintf("The compute will be stopped while it is resized to %s, then started again. Expect a short outage.",
				plan.Specification.ValueString()),
		)
	}
}

// validateCatalog checks image / region / specification against the live catalog
// at plan time, so a bad slug fails here instead of mid-apply. It skips fields
// that are unknown or (on update) unchanged. A fetch error is non-fatal: the API
// stays the final authority at apply.
func (r *computeResource) validateCatalog(ctx context.Context, plan computeResourceModel, state *computeResourceModel, resp *resource.ModifyPlanResponse) {
	needsCheck := func(planned types.String, prior func() types.String) bool {
		if planned.IsUnknown() || planned.IsNull() {
			return false
		}
		if state != nil && planned.Equal(prior()) {
			return false
		}
		return true
	}

	if needsCheck(plan.Image, func() types.String { return state.Image }) {
		if images, err := r.client.ListImages(ctx); err == nil {
			slugs := make([]string, len(images))
			for i, im := range images {
				slugs[i] = im.Image
			}
			if !contains(slugs, plan.Image.ValueString()) {
				resp.Diagnostics.AddAttributeError(path.Root("image"), "Unknown image",
					fmt.Sprintf("%q is not a known image. Valid values: %s.", plan.Image.ValueString(), joinSorted(slugs)))
			}
		}
	}

	if needsCheck(plan.Region, func() types.String { return state.Region }) {
		if regions, err := r.client.ListRegions(ctx); err == nil {
			slugs := make([]string, len(regions))
			for i, rg := range regions {
				slugs[i] = rg.Region
			}
			if !contains(slugs, plan.Region.ValueString()) {
				resp.Diagnostics.AddAttributeError(path.Root("region"), "Unknown region",
					fmt.Sprintf("%q is not a known region. Valid values: %s.", plan.Region.ValueString(), joinSorted(slugs)))
			}
		}
	}

	if needsCheck(plan.Specification, func() types.String { return state.Specification }) {
		if specs, err := r.client.ListAllSpecifications(ctx); err == nil {
			slugs := make([]string, len(specs))
			for i, s := range specs {
				slugs[i] = s.Specification
			}
			if !contains(slugs, plan.Specification.ValueString()) {
				resp.Diagnostics.AddAttributeError(specificationPath(), "Unknown specification",
					fmt.Sprintf("%q is not a known specification. Valid values: %s.", plan.Specification.ValueString(), joinSorted(slugs)))
			}
		}
	}
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// joinSorted returns the values sorted and comma-joined, for a readable "valid
// values: ..." list in a diagnostic.
func joinSorted(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}
