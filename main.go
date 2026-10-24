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

// version is set by GoReleaser at release (-X main.version=...).
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/plotwork-official/plotwork",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
