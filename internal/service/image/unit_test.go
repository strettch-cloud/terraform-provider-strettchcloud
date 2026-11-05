package image_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

// mockImages serves GET /images with the given JSON data array (envelope-wrapped),
// or an error status when status != 200.
func mockImages(t *testing.T, status int, dataJSON string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":` + dataJSON + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func providerBlock(url string) string {
	return fmt.Sprintf(`provider "strettchcloud" {
  api_key = "unit-test"
  api_url = %q
}
`, url)
}

const twoUbuntu = `[{"image":"UBUNTU-24.04","imageName":"Ubuntu","version":"24.04","displayName":"Ubuntu 24.04 LTS"},
{"image":"UBUNTU-22.04","imageName":"Ubuntu","version":"22.04","displayName":"Ubuntu 22.04 LTS"}]`

func TestUnitImage_Success(t *testing.T) {
	url := mockImages(t, http.StatusOK, twoUbuntu)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_image" "t" { image = "UBUNTU-24.04" }`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_image.t", "display_name", "Ubuntu 24.04 LTS"),
		}},
	})
}

func TestUnitImage_NoMatch(t *testing.T) {
	url := mockImages(t, http.StatusOK, twoUbuntu)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_image" "t" { image = "NOPE" }`,
			ExpectError: regexp.MustCompile("No matching image"),
		}},
	})
}

func TestUnitImage_Ambiguous(t *testing.T) {
	url := mockImages(t, http.StatusOK, twoUbuntu)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_image" "t" { image_name = "Ubuntu" }`,
			ExpectError: regexp.MustCompile("Ambiguous image"),
		}},
	})
}

func TestUnitImage_APIError(t *testing.T) {
	url := mockImages(t, http.StatusUnauthorized, "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_image" "t" { image = "X" }`,
			ExpectError: regexp.MustCompile("Reading images failed"),
		}},
	})
}

func TestUnitImage_NoMatchAllFilters(t *testing.T) {
	// Exercises every branch of describeFilters.
	url := mockImages(t, http.StatusOK, twoUbuntu)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + "data \"strettchcloud_image\" \"t\" {\n  image = \"A\"\n  image_name = \"B\"\n  version = \"C\"\n}",
			ExpectError: regexp.MustCompile("image=A, image_name=B, version=C"),
		}},
	})
}

func TestUnitImages_List(t *testing.T) {
	url := mockImages(t, http.StatusOK, twoUbuntu)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_images" "all" {}`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_images.all", "images.#", "2"),
		}},
	})
}

func TestUnitImages_ListAPIError(t *testing.T) {
	url := mockImages(t, http.StatusUnauthorized, "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_images" "all" {}`,
			ExpectError: regexp.MustCompile("Reading images failed"),
		}},
	})
}
