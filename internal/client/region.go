package client

import (
	"context"
	"net/http"
)

// Region mirrors the API's RegionResponse. region is the slug the compute
// resource uses (e.g. KGL-1).
type Region struct {
	ID          int    `json:"id"`
	Region      string `json:"region"`
	CountryCode string `json:"countryCode"`
	Name        string `json:"name"`
}

// ListRegions fetches the region catalog (GET /regions, a plain array, not
// paginated).
func (c *Client) ListRegions(ctx context.Context) ([]Region, error) {
	var out []Region
	_, err := c.do(ctx, http.MethodGet, "/regions", nil, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return out, nil
}
