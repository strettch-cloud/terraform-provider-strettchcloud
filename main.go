// Command terraform-provider-strettchcloud is the Strettch Cloud Terraform
// provider plugin. It serves the provider over the Terraform plugin protocol.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/strettch-cloud/terraform-provider-strettchcloud/internal/provider"
)

// version is set to the released version by GoReleaser at build time. It is
// "dev" for local builds and feeds the User-Agent the provider sends on every
// request (see internal/provider).
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/strettch-cloud/strettchcloud",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
