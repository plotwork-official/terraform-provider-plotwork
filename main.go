// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

// Command terraform-provider-plotwork serves the Plotwork provider to
// Terraform and OpenTofu over the plugin protocol.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/provider"
)

// version and defaultAPIURL are set by GoReleaser at release
// (-X main.version=...). A pre-release build defaults to the preview API, a
// stable one to production; api_url and PLOTWORK_API_URL override either.
var (
	version       = "dev"
	defaultAPIURL = "https://api.plotwork.dev"
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version, defaultAPIURL), providerserver.ServeOpts{
		Address: "registry.terraform.io/plotwork-official/plotwork",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
