package svcutil

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/client"
)

func TestDataSourceClient(t *testing.T) {
	c := client.NewClient(client.Config{})

	// nil ProviderData -> nil, no error (first configure pass).
	resp := &datasource.ConfigureResponse{}
	if got := DataSourceClient(datasource.ConfigureRequest{}, resp); got != nil || resp.Diagnostics.HasError() {
		t.Error("nil ProviderData should return nil without error")
	}

	// correct type -> client.
	if got := DataSourceClient(datasource.ConfigureRequest{ProviderData: c}, &datasource.ConfigureResponse{}); got != c {
		t.Error("should return the client")
	}

	// wrong type -> nil + error.
	resp2 := &datasource.ConfigureResponse{}
	if got := DataSourceClient(datasource.ConfigureRequest{ProviderData: "nope"}, resp2); got != nil || !resp2.Diagnostics.HasError() {
		t.Error("wrong type should add an error")
	}
}

func TestResourceClient(t *testing.T) {
	c := client.NewClient(client.Config{})
	if got := ResourceClient(resource.ConfigureRequest{}, &resource.ConfigureResponse{}); got != nil {
		t.Error("nil -> nil")
	}
	if got := ResourceClient(resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{}); got != c {
		t.Error("should return client")
	}
	resp := &resource.ConfigureResponse{}
	if ResourceClient(resource.ConfigureRequest{ProviderData: 42}, resp); !resp.Diagnostics.HasError() {
		t.Error("wrong type should error")
	}
}

func TestActionClient(t *testing.T) {
	c := client.NewClient(client.Config{})
	if got := ActionClient(action.ConfigureRequest{}, &action.ConfigureResponse{}); got != nil {
		t.Error("nil -> nil")
	}
	if got := ActionClient(action.ConfigureRequest{ProviderData: c}, &action.ConfigureResponse{}); got != c {
		t.Error("should return client")
	}
	resp := &action.ConfigureResponse{}
	if ActionClient(action.ConfigureRequest{ProviderData: 42}, resp); !resp.Diagnostics.HasError() {
		t.Error("wrong type should error")
	}
}
