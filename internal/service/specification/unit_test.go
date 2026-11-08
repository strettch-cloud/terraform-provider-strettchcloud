package specification_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	scacctest "github.com/strettch-cloud/terraform-provider-strettchcloud/internal/acctest"
)

const specCatalog = `[{"id":"a","specification":"S-1C-1G","vcpus":1,"memoryMb":1024,"storageGb":20},
{"id":"b","specification":"S-1C-2G","vcpus":1,"memoryMb":2048,"storageGb":40},
{"id":"c","specification":"S-2C-4G","vcpus":2,"memoryMb":4096,"storageGb":80}]`

func mockSpecs(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"success":false,"code":"UNAUTHORIZED","message":"no"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":` + specCatalog + `,"meta":{"total":3,"page":1,"limit":100}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func providerBlock(url string) string {
	return fmt.Sprintf("provider \"strettchcloud\" {\n  api_key = \"unit-test\"\n  api_url = %q\n}\n", url)
}

func TestUnitSpec_BySlug(t *testing.T) {
	url := mockSpecs(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_specification" "t" { specification = "S-1C-1G" }`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_specification.t", "vcpus", "1"),
		}},
	})
}

func TestUnitSpec_BySize(t *testing.T) {
	url := mockSpecs(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + "data \"strettchcloud_specification\" \"t\" {\n  vcpus = 2\n  memory_mb = 4096\n}",
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_specification.t", "specification", "S-2C-4G"),
		}},
	})
}

func TestUnitSpec_NoMatchAllFilters(t *testing.T) {
	url := mockSpecs(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + "data \"strettchcloud_specification\" \"t\" {\n  specification = \"X\"\n  vcpus = 9\n  memory_mb = 9\n}",
			ExpectError: regexp.MustCompile("specification=X, vcpus=9"),
		}},
	})
}

func TestUnitSpec_Ambiguous(t *testing.T) {
	url := mockSpecs(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_specification" "t" { vcpus = 1 }`, // two specs share vcpus=1
			ExpectError: regexp.MustCompile("Ambiguous specification"),
		}},
	})
}

func TestUnitSpec_APIError(t *testing.T) {
	url := mockSpecs(t, http.StatusUnauthorized)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_specification" "t" { specification = "S-1C-1G" }`,
			ExpectError: regexp.MustCompile("Reading specifications failed"),
		}},
	})
}

func TestUnitSpecs_List(t *testing.T) {
	url := mockSpecs(t, http.StatusOK)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `data "strettchcloud_specifications" "all" {}`,
			Check:  resource.TestCheckResourceAttr("data.strettchcloud_specifications.all", "specifications.#", "3"),
		}},
	})
}

func TestUnitSpecs_ListAPIError(t *testing.T) {
	url := mockSpecs(t, http.StatusUnauthorized)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: scacctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerBlock(url) + `data "strettchcloud_specifications" "all" {}`,
			ExpectError: regexp.MustCompile("Reading specifications failed"),
		}},
	})
}
