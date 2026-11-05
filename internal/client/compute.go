package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Compute states. Transitional values never reach Terraform state; the resource
// layer writes only stable ones.
const (
	StateStarting   = "STARTING"
	StateRunning    = "RUNNING"
	StateStopping   = "STOPPING"
	StateStopped    = "STOPPED"
	StateRebooting  = "REBOOTING"
	StateUpgrading  = "UPGRADING"
	StateDestroying = "DESTROYING"
	StateDestroyed  = "DESTROYED"
)

// IsStableState reports whether s is a stable (non-transitional) state.
func IsStableState(s string) bool {
	switch s {
	case StateRunning, StateStopped, StateDestroyed:
		return true
	default:
		return false
	}
}

// Compute mirrors the public API's ComputeResponse. Fields the API does not yet
// return — PublicKeys and WithPublicIP — are included for the target contract
// and decode to nil when absent; the resource layer then keeps prior state.
type Compute struct {
	ID            string   `json:"id"`
	HostName      string   `json:"hostName"`
	State         string   `json:"state"`
	Image         string   `json:"image"`
	Specification string   `json:"specification"`
	Region        string   `json:"region"`
	IPv4          string   `json:"ipv4"`
	PrivateIPv4   string   `json:"privateIpv4"`
	Tags          []string `json:"tags"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`

	// Target-contract additions, not yet returned by the live API.
	PublicKeys   []string `json:"publicKeys,omitempty"`
	WithPublicIP *bool    `json:"withPublicIp,omitempty"`
}

// ComputeCreateRequest is the POST /computes body. state is never sent; every
// new compute starts.
type ComputeCreateRequest struct {
	HostName      string   `json:"hostName"`
	Image         string   `json:"image"`
	Region        string   `json:"region"`
	Specification string   `json:"specification"`
	Tags          []string `json:"tags,omitempty"`
	WithPublicIP  bool     `json:"withPublicIp"`
	PublicKeys    []string `json:"publicKeys"`
}

// ComputeUpdateRequest is the PATCH /computes/{id} body for the in-place update
// of host_name and tags (target contract). Both fields are optional; a nil
// pointer / nil slice means "leave unchanged".
type ComputeUpdateRequest struct {
	HostName *string   `json:"hostName,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
}

// upsizeRequest is the PATCH /computes/{id}/upsize body. The API field is
// newSpecification (confirmed in public-api).
type upsizeRequest struct {
	NewSpecification string `json:"newSpecification"`
}

// CreateCompute provisions a compute (POST /computes, async 202). The caller
// supplies the Idempotency-Key, reused across this request's retries.
func (c *Client) CreateCompute(ctx context.Context, req ComputeCreateRequest, idempotencyKey string) (*Compute, error) {
	var out Compute
	_, err := c.do(ctx, http.MethodPost, "/computes", req, &out, requestOptions{idempotencyKey: idempotencyKey})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCompute fetches one compute (GET /computes/{id}). A 404 is returned as an
// APIError; the caller decides what it means.
func (c *Client) GetCompute(ctx context.Context, id string) (*Compute, error) {
	var out Compute
	_, err := c.do(ctx, http.MethodGet, "/computes/"+url.PathEscape(id), nil, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListComputes fetches one page of computes (GET /computes). page and limit are
// 1-based; the API caps limit at 100.
func (c *Client) ListComputes(ctx context.Context, page, limit int) ([]Compute, *PaginationMeta, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", strconv.Itoa(limit))
	var out []Compute
	meta, err := c.do(ctx, http.MethodGet, "/computes", nil, &out, requestOptions{query: q})
	if err != nil {
		return nil, nil, err
	}
	return out, meta, nil
}

// ListAllComputes walks every page of GET /computes and returns the full set.
// Used by the list resource.
func (c *Client) ListAllComputes(ctx context.Context) ([]Compute, error) {
	const limit = 100
	var all []Compute
	for page := 1; ; page++ {
		items, meta, err := c.ListComputes(ctx, page, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if meta == nil || len(items) == 0 || len(all) >= meta.Total || len(items) < limit {
			break
		}
	}
	return all, nil
}

// UpdateCompute changes host_name and/or tags in place (PATCH /computes/{id},
// synchronous 200, target contract).
func (c *Client) UpdateCompute(ctx context.Context, id string, req ComputeUpdateRequest) (*Compute, error) {
	var out Compute
	_, err := c.do(ctx, http.MethodPatch, "/computes/"+url.PathEscape(id), req, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsizeCompute raises the specification (PATCH /computes/{id}/upsize, async).
// The API stops a running compute first and always leaves it STOPPED.
func (c *Client) UpsizeCompute(ctx context.Context, id, newSpecification string) (*Compute, error) {
	var out Compute
	_, err := c.do(ctx, http.MethodPatch, "/computes/"+url.PathEscape(id)+"/upsize", upsizeRequest{NewSpecification: newSpecification}, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// StartCompute powers a compute on (POST /computes/{id}/start, async).
func (c *Client) StartCompute(ctx context.Context, id string) (*Compute, error) {
	return c.computeAction(ctx, id, "start")
}

// StopCompute shuts a compute down (POST /computes/{id}/stop, async).
func (c *Client) StopCompute(ctx context.Context, id string) (*Compute, error) {
	return c.computeAction(ctx, id, "stop")
}

// RebootCompute reboots a compute (POST /computes/{id}/reboot, async). Rejected
// by the API on a stopped compute.
func (c *Client) RebootCompute(ctx context.Context, id string) (*Compute, error) {
	return c.computeAction(ctx, id, "reboot")
}

func (c *Client) computeAction(ctx context.Context, id, action string) (*Compute, error) {
	var out Compute
	_, err := c.do(ctx, http.MethodPost, "/computes/"+url.PathEscape(id)+"/"+action, nil, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCompute deletes a compute (DELETE /computes/{id}, async). A 404 means it
// is already gone, which Delete treats as success.
func (c *Client) DeleteCompute(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/computes/"+url.PathEscape(id), nil, nil, requestOptions{})
	if err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

// WaitForState polls until the compute reaches a stable state, bounded by ctx. It
// returns the compute at target, keeps polling while it is still in from (the
// pre-action state, before the API applies the transition), and errors on any
// other stable state. For target StateDestroyed, a 404 is success and returns nil.
func (c *Client) WaitForState(ctx context.Context, id, from, target string) (*Compute, error) {
	start := time.Now()
	for i := 0; ; i++ {
		if err := sleep(ctx, c.pollScheduleFn(i)); err != nil {
			return nil, fmt.Errorf("timed out after %s waiting for compute %s to reach %s: %w",
				time.Since(start).Round(time.Second), id, target, err)
		}

		compute, err := c.GetCompute(ctx, id)
		if err != nil {
			if target == StateDestroyed && IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}

		tflog.Debug(ctx, "poll compute state", map[string]any{
			"id":      id,
			"state":   compute.State,
			"elapsed": time.Since(start).Round(time.Second).String(),
		})

		if !IsStableState(compute.State) {
			continue
		}

		if compute.State == target {
			return compute, nil
		}
		if compute.State == from {
			// Pre-action state; the transition may not have applied yet. Keep polling.
			continue
		}
		return nil, fmt.Errorf("compute %s reached %s while waiting for %s (waited %s); check the compute in the dashboard",
			id, compute.State, target, time.Since(start).Round(time.Second))
	}
}

// WaitForSpecification polls until the compute reaches the given specification and
// a stable state, bounded by ctx. Used after an upsize: the compute is STOPPED both
// before and after, so the specification — not the state — signals completion.
func (c *Client) WaitForSpecification(ctx context.Context, id, specification string) (*Compute, error) {
	start := time.Now()
	for i := 0; ; i++ {
		if err := sleep(ctx, c.pollScheduleFn(i)); err != nil {
			return nil, fmt.Errorf("timed out after %s waiting for compute %s to resize to %s: %w",
				time.Since(start).Round(time.Second), id, specification, err)
		}

		compute, err := c.GetCompute(ctx, id)
		if err != nil {
			return nil, err
		}

		tflog.Debug(ctx, "poll compute specification", map[string]any{
			"id":            id,
			"state":         compute.State,
			"specification": compute.Specification,
			"elapsed":       time.Since(start).Round(time.Second).String(),
		})

		if !IsStableState(compute.State) {
			continue
		}
		if compute.Specification == specification {
			return compute, nil
		}
		// Stable but still the old specification: the upsize has not taken effect
		// yet. Keep polling.
	}
}

// WaitForReboot polls until the compute is RUNNING again after a reboot, bounded by
// ctx. It is RUNNING both before and after, so a changed updatedAt (vs
// priorUpdatedAt, taken before the reboot) signals completion; DESTROYED or any
// other stable state is an error.
func (c *Client) WaitForReboot(ctx context.Context, id, priorUpdatedAt string) (*Compute, error) {
	start := time.Now()
	for i := 0; ; i++ {
		if err := sleep(ctx, c.pollScheduleFn(i)); err != nil {
			return nil, fmt.Errorf("timed out after %s waiting for compute %s to return to RUNNING after reboot: %w",
				time.Since(start).Round(time.Second), id, err)
		}

		compute, err := c.GetCompute(ctx, id)
		if err != nil {
			return nil, err
		}

		tflog.Debug(ctx, "poll compute state (reboot)", map[string]any{
			"id":      id,
			"state":   compute.State,
			"elapsed": time.Since(start).Round(time.Second).String(),
		})

		if !IsStableState(compute.State) {
			continue
		}
		switch compute.State {
		case StateRunning:
			if compute.UpdatedAt != priorUpdatedAt {
				return compute, nil
			}
			// Still the pre-reboot RUNNING (updatedAt unchanged): the reboot has
			// not been applied yet. Keep polling.
		case StateDestroyed:
			return nil, fmt.Errorf("compute %s reached DESTROYED before returning to RUNNING (waited %s)",
				id, time.Since(start).Round(time.Second))
		default:
			return nil, fmt.Errorf("compute %s reached %s while waiting to return to RUNNING after reboot (waited %s)",
				id, compute.State, time.Since(start).Round(time.Second))
		}
	}
}

// WaitForRunning polls until a freshly created compute reaches RUNNING, bounded by
// ctx. Unlike WaitForState it treats STOPPED as transient (a new compute is created
// STOPPED, then auto-started); DESTROYED or a 404 is an error. Used by Create.
func (c *Client) WaitForRunning(ctx context.Context, id string) (*Compute, error) {
	start := time.Now()
	for i := 0; ; i++ {
		if err := sleep(ctx, c.pollScheduleFn(i)); err != nil {
			return nil, fmt.Errorf("timed out after %s waiting for compute %s to reach RUNNING: %w",
				time.Since(start).Round(time.Second), id, err)
		}

		compute, err := c.GetCompute(ctx, id)
		if err != nil {
			return nil, err
		}

		tflog.Debug(ctx, "poll compute state (create)", map[string]any{
			"id":      id,
			"state":   compute.State,
			"elapsed": time.Since(start).Round(time.Second).String(),
		})

		switch compute.State {
		case StateRunning:
			return compute, nil
		case StateDestroyed:
			return nil, fmt.Errorf("compute %s reached DESTROYED before RUNNING (waited %s)",
				id, time.Since(start).Round(time.Second))
		default:
			// STARTING, STOPPED, UPGRADING, ... — still settling; keep polling.
			continue
		}
	}
}
