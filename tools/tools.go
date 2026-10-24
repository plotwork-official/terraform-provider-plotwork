// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

//go:build generate

// Package tools pins the tools `make generate` runs, in their own module so
// they do not enter the provider's dependencies.
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Format the examples the docs embed.
//go:generate terraform fmt -recursive ../examples/

// Write docs/ from the provider schema, templates/ and examples/.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. --provider-name plotwork
