// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// protoV6ProviderFactories serves the provider to the Terraform CLI that
// terraform-plugin-testing runs, in process.
var protoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"plotwork": providerserver.NewProtocol6WithError(New("test")()),
}

// These tests drive the real Terraform CLI against an in-memory fake of the
// Plotwork API, so they need terraform on PATH and no network or account.

func TestImportDataSource(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
data "plotwork_import" "net" {
  format = "terraform"
  files = {
    "main.tf"    = "resource \"aws_vpc\" \"main\" {}"
    "network.tf" = "resource \"aws_subnet\" \"a\" {}"
  }
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.plotwork_import.net", "kind", "flow"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "source", "flow\n# terraform: main.tf,network.tf"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "summary", "A flow of 2 boxes"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "complete", "false"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "coverage", "Drawn 2 of 3 resources."),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "warnings.#", "1"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "warnings.0", "I201 3:2 A count was left out"),
				resource.TestCheckResourceAttr("data.plotwork_import.net", "outline.#", "2"),
				resource.TestCheckResourceAttrSet("data.plotwork_import.net", "id"),
			),
		}},
	})
}

func TestImportDataSourceReportsAFileItCannotRead(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
data "plotwork_import" "bad" {
  format = "terraform"
  text   = "unreadable"
}`,
			ExpectError: regexp.MustCompile(`(?s)Plotwork could not import the files.*I101 1:1 The file could not be read`),
		}},
	})
}

func TestImportDataSourceNeedsTextOrFiles(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
data "plotwork_import" "none" {
  format = "terraform"
}`,
			ExpectError: regexp.MustCompile(`No attribute specified when one \(and only one\) of`),
		}},
	})
}

func TestRenderDataSource(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
data "plotwork_render" "net" {
  source = "flow"
  scheme = "auto"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.plotwork_render.net", "svg", `<svg data-scheme="auto">flow</svg>`),
				resource.TestCheckResourceAttr("data.plotwork_render.net", "width", "640"),
				resource.TestCheckResourceAttr("data.plotwork_render.net", "height", "480"),
				resource.TestCheckResourceAttr("data.plotwork_render.net", "warnings.#", "0"),
			),
		}},
	})
}

func TestRenderDataSourceShowsTheSourceErrors(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      api.providerBlock("") + `data "plotwork_render" "bad" { source = "broken" }`,
			ExpectError: regexp.MustCompile(`(?s)diagram.invalid.*E101 2:1 Unknown statement Hint: Write a box`),
		}},
	})
}

func TestMissingAPIKeySaysHowToSetOne(t *testing.T) {
	api := newFakeAPI(t)
	t.Setenv(envAPIKey, "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
provider "plotwork" {
  api_url = "` + api.srv.URL + `"
}
data "plotwork_render" "net" { source = "flow" }`,
			ExpectError: regexp.MustCompile(`(?s)Missing Plotwork API key.*` + envAPIKey),
		}},
	})
}

func TestInvalidAPIKeyIsExplained(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
provider "plotwork" {
  api_url = "` + api.srv.URL + `"
  api_key = "pw_wrong"
}
data "plotwork_render" "net" { source = "flow" }`,
			ExpectError: regexp.MustCompile(`(?s)HTTP 401.*revoked or expired`),
		}},
	})
}

func TestDiagramLifecycle(t *testing.T) {
	api := newFakeAPI(t)
	config := func(source string) string {
		return api.providerBlock("") + fmt.Sprintf(`
resource "plotwork_diagram" "net" {
  source = %q
  name   = "Network"
  folder = "Platform/Network"
  alias  = "prod-network"
}`, source)
	}
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if held := api.held(); len(held) != 0 {
				return fmt.Errorf("destroy left %d diagrams in Plotwork", len(held))
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config("flow one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_diagram.net", "library", "personal"),
					resource.TestCheckResourceAttr("plotwork_diagram.net", "scheme", "light"),
					resource.TestCheckResourceAttr("plotwork_diagram.net", "stable_url", api.srv.URL+"/l/prod-network.svg"),
					resource.TestCheckNoResourceAttr("plotwork_diagram.net", "view_url"),
					resource.TestCheckResourceAttrSet("plotwork_diagram.net", "expires_at"),
					func(s *terraform.State) error {
						firstID = s.RootModule().Resources["plotwork_diagram.net"].Primary.ID
						d := api.held()[firstID]
						if d == nil || d.name != "Network" || d.folder != "Platform/Network" {
							return fmt.Errorf("Plotwork holds %+v, want the named diagram in Platform/Network", d)
						}
						return resource.TestCheckResourceAttr("plotwork_diagram.net", "url", api.srv.URL+"/d/"+firstID+".svg")(s)
					},
				),
			},
			{
				// A changed source is a new diagram; the old one is deleted and the stable link stays.
				Config: config("flow two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_diagram.net", "stable_url", api.srv.URL+"/l/prod-network.svg"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources["plotwork_diagram.net"].Primary.ID
						held := api.held()
						if id == firstID {
							return fmt.Errorf("the id did not change with the source")
						}
						if _, ok := held[firstID]; ok {
							return fmt.Errorf("the replaced diagram %s is still held", firstID)
						}
						if len(held) != 1 || held[id] == nil {
							return fmt.Errorf("Plotwork holds %d diagrams, want only %s", len(held), id)
						}
						return nil
					},
				),
			},
			{
				// The same configuration again plans nothing.
				Config:   config("flow two"),
				PlanOnly: true,
			},
		},
	})
}

func TestDiagramDeletedOutsideTerraformIsPublishedAgain(t *testing.T) {
	api := newFakeAPI(t)
	config := api.providerBlock("") + `resource "plotwork_diagram" "net" { source = "flow" }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					for id := range api.held() {
						api.forget(id)
					}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: func(*terraform.State) error {
					if len(api.held()) != 1 {
						return fmt.Errorf("the diagram was not published again")
					}
					return nil
				},
			},
		},
	})
}

func TestDiagramInAnOrganisationLibrary(t *testing.T) {
	api := newFakeAPI(t)
	config := func(source string) string {
		return api.providerBlock(`organization = "the-team"`) + fmt.Sprintf(`
resource "plotwork_diagram" "net" {
  source         = %q
  classification = "internal"
}`, source)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if held := api.held(); len(held) != 0 {
				return fmt.Errorf("destroy left %d diagrams in the library", len(held))
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config("flow one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_diagram.net", "library", "the-team"),
					resource.TestCheckNoResourceAttr("plotwork_diagram.net", "url"),
					resource.TestCheckNoResourceAttr("plotwork_diagram.net", "expires_at"),
					resource.TestCheckResourceAttrSet("plotwork_diagram.net", "view_url"),
				),
			},
			{
				Config: config("flow two"),
				Check: func(*terraform.State) error {
					if held := api.held(); len(held) != 1 {
						return fmt.Errorf("the library holds %d diagrams after an update, want 1", len(held))
					}
					return nil
				},
			},
		},
	})
}

func TestDiagramRoutedToAnOrganisationIsDeletedThere(t *testing.T) {
	api := newFakeAPI(t)
	api.routeTo = "the-team"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if held := api.held(); len(held) != 0 {
				return fmt.Errorf("destroy left %d diagrams in the library", len(held))
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `resource "plotwork_diagram" "net" { source = "flow" }`,
			Check:  resource.TestCheckResourceAttr("plotwork_diagram.net", "library", "org-id-the-team"),
		}},
	})
}

func TestAliasIsRefusedForAnOrganisation(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: api.providerBlock("") + `
resource "plotwork_diagram" "net" {
  source       = "flow"
  alias        = "prod-network"
  organization = "the-team"
}`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: api.providerBlock(`organization = "the-team"`) + `
resource "plotwork_diagram" "net" {
  source = "flow"
  alias  = "prod-network"
}`,
				ExpectError: regexp.MustCompile(`alias makes a public stable link`),
			},
		},
	})
}

func TestAliasMustBeLowerCaseWords(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
resource "plotwork_diagram" "net" {
  source = "flow"
  alias  = "Prod_Network"
}`,
			ExpectError: regexp.MustCompile(`lower-case letters and digits`),
		}},
	})
}

func TestDiagramImport(t *testing.T) {
	api := newFakeAPI(t)
	config := api.providerBlock(`organization = "the-team"`) + `resource "plotwork_diagram" "net" { source = "flow" }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// An organisation's diagram imports as "<organisation>/<id>".
				ResourceName: "plotwork_diagram.net",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return "the-team/" + s.RootModule().Resources["plotwork_diagram.net"].Primary.ID, nil
				},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["library"] != "the-team" {
						return fmt.Errorf("imported %+v, want a diagram in the-team", states)
					}
					return nil
				},
			},
			{
				// One the library does not hold is refused.
				ResourceName:  "plotwork_diagram.net",
				ImportState:   true,
				ImportStateId: "the-team/missing",
				ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
			},
		},
	})
}
