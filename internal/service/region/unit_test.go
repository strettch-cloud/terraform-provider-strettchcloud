package region_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

func mockRegions(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":1,"region":"KGL-1","countryCode":"RW","name":"Kigali-1"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func providerBlock(url string) string {
	return fmt.Sprintf("provider \"strettchcloud\" {\n  api_key = \"unit-test\"\n  api_url = %q\n}\n", url)
}

func TestUnitRegion_Success(t *testing.T) {
	url := mockRegions(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_region" "t" { region = "KGL-1" }`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_region.t", "country_code", "RW"),
		}},
	})
}

func TestUnitRegion_NoMatch(t *testing.T) {
	url := mockRegions(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_region" "t" { region = "NOPE" }`,
			ExpectError: regexp.MustCompile("No matching region"),
		}},
	})
}

func TestUnitRegion_APIError(t *testing.T) {
	url := mockRegions(t, http.StatusUnauthorized)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_region" "t" { region = "KGL-1" }`,
			ExpectError: regexp.MustCompile("Reading regions failed"),
		}},
	})
}

func TestUnitRegions_List(t *testing.T) {
	url := mockRegions(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_regions" "all" {}`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_regions.all", "regions.#", "1"),
		}},
	})
}

func TestUnitRegions_ListAPIError(t *testing.T) {
	url := mockRegions(t, http.StatusUnauthorized)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_regions" "all" {}`,
			ExpectError: regexp.MustCompile("Reading regions failed"),
		}},
	})
}
