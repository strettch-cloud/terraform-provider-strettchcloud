package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Specification mirrors the API's SpecificationResponse. memoryMb and storageGb
// are floats in the live API, not integers, so they are float64 here.
type Specification struct {
	ID            string  `json:"id"`
	Specification string  `json:"specification"`
	VCPUs         int     `json:"vcpus"`
	MemoryMB      float64 `json:"memoryMb"`
	StorageGB     float64 `json:"storageGb"`
}

// ListAllSpecifications walks every page of GET /computes/specifications
// (paginated) and returns the full catalog.
func (c *Client) ListAllSpecifications(ctx context.Context) ([]Specification, error) {
	const limit = 100
	var all []Specification
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", strconv.Itoa(limit))

		var items []Specification
		meta, err := c.do(ctx, http.MethodGet, "/computes/specifications", nil, &items, requestOptions{query: q})
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
