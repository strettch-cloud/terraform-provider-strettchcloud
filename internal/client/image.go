package client

import (
	"context"
	"net/http"
)

// Image mirrors the API's ImageResponse. image is the combined slug the compute
// resource uses (e.g. UBUNTU-24.04).
type Image struct {
	Image       string `json:"image"`
	ImageName   string `json:"imageName"`
	Version     string `json:"version"`
	DisplayName string `json:"displayName"`
}

// ListImages fetches the image catalog (GET /images, a plain array, not
// paginated).
func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	var out []Image
	_, err := c.do(ctx, http.MethodGet, "/images", nil, &out, requestOptions{})
	if err != nil {
		return nil, err
	}
	return out, nil
}
