// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// These tests drive the organisation design resources through the real
// Terraform CLI against the fake API (fake_design_api_test.go).

func TestBrandLifecycle(t *testing.T) {
	api := newFakeAPI(t)
	config := func(colours string) string {
		return api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_brand" "acme" {
  ` + colours + `
}`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := api.brandHeld(); ok {
				return fmt.Errorf("destroy left the brand")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Upper case is sent as lower case; the derived colours are results.
				Config: config(`primary = "#2A78D6"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "id", "acme"),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "organization", "acme"),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "primary", "#2A78D6"),
					resource.TestCheckNoResourceAttr("plotwork_organization_brand.acme", "secondary"),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "effective_primary", "#2a78d6"),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "effective_secondary", fakeDerivedSecondary),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "effective_accent", fakeDerivedAccent),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "active", "true"),
					func(*terraform.State) error {
						if b, _ := api.brandHeld(); b.primary != "#2a78d6" || b.secondary != "" {
							return fmt.Errorf("Plotwork holds %+v, want only a lower-case primary", b)
						}
						return nil
					},
				),
			},
			{
				// The case of a stored colour is no change.
				Config:   config(`primary = "#2A78D6"`),
				PlanOnly: true,
			},
			{
				// A chosen secondary in the short form is sent written out.
				Config: config(`
  primary   = "#2A78D6"
  secondary = "#ABC"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "secondary", "#ABC"),
					resource.TestCheckResourceAttr("plotwork_organization_brand.acme", "effective_secondary", "#aabbcc"),
					func(*terraform.State) error {
						if b, _ := api.brandHeld(); b.secondary != "#aabbcc" {
							return fmt.Errorf("Plotwork holds secondary %q, want #aabbcc", b.secondary)
						}
						return nil
					},
				),
			},
			{
				Config: config(`
  primary   = "#2A78D6"
  secondary = "#ABC"`),
				PlanOnly: true,
			},
			{
				ResourceName:  "plotwork_organization_brand.acme",
				ImportState:   true,
				ImportStateId: "acme",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["primary"] != "#2a78d6" || states[0].Attributes["secondary"] != "#aabbcc" {
						return fmt.Errorf("imported %+v, want the stored colours", states)
					}
					return nil
				},
			},
		},
	})
}

func TestBrandRemovedOutsideTerraformIsSavedAgain(t *testing.T) {
	api := newFakeAPI(t)
	config := api.providerBlock("") + `
resource "plotwork_organization_brand" "acme" {
  organization = "acme"
  primary      = "#2a78d6"
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.mu.Lock()
					delete(api.brands, "acme")
					api.mu.Unlock()
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: func(*terraform.State) error {
					if _, ok := api.brandHeld(); !ok {
						return fmt.Errorf("the brand was not saved again")
					}
					return nil
				},
			},
		},
	})
}

func TestBrandShowsTheChecksIssues(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_brand" "acme" {
  primary   = "#2a78d6"
  secondary = "#2A78D6"
}`,
			ExpectError: regexp.MustCompile(`(?s)brand.invalid.*error secondary \(too-similar\): The secondary is the primary`),
		}},
	})
}

func TestBrandColourMustBeHex(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_brand" "acme" {
  primary = "blue"
}`,
			ExpectError: regexp.MustCompile(`must be a colour written #rrggbb or #rgb`),
		}},
	})
}

func TestDesignNeedsTheDesignWriteScope(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
provider "plotwork" {
  api_url      = "` + api.srv.URL + `"
  api_key      = "` + fakeNoDesignKey + `"
  organization = "acme"
}
resource "plotwork_organization_brand" "acme" {
  primary = "#2a78d6"
}`,
			ExpectError: regexp.MustCompile(`(?s)orgs.forbidden.*design:write scope`),
		}},
	})
}

func TestDesignNeedsAnOrganisation(t *testing.T) {
	api := newFakeAPI(t)
	t.Setenv(envOrganization, "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock("") + `
resource "plotwork_organization_icon" "x" {
  name = "payments-api"
  svg  = "<svg/>"
}`,
			ExpectError: regexp.MustCompile(`No organisation`),
		}},
	})
}

// paymentsSVG is an icon file for the tests.
const paymentsSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`

func TestIconLifecycle(t *testing.T) {
	api := newFakeAPI(t)
	config := func(svg, aliases string) string {
		return api.providerBlock(`organization = "acme"`) + fmt.Sprintf(`
resource "plotwork_organization_icon" "payments" {
  name    = "payments-api"
  svg     = %q
  aliases = %s
}`, svg, aliases)
	}
	changed := strings.Replace(paymentsSVG, `r="10"`, `r="11"`, 1)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := api.paymentsIconHeld(); ok {
				return fmt.Errorf("destroy left the icon")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(paymentsSVG, `["payments service"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "id", "acme/payments-api"),
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "ref", "org:payments-api"),
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "content_sha256", sha256Hex(paymentsSVG)),
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "media_type", "image/svg+xml"),
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "width", "24"),
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "bytes", fmt.Sprint(len(paymentsSVG))),
					resource.TestCheckResourceAttrSet("plotwork_organization_icon.payments", "updated_at"),
				),
			},
			{
				// Only the aliases change: the file is not sent again.
				Config: config(paymentsSVG, `["payments service", "billing"]`),
				Check: func(*terraform.State) error {
					i, _ := api.paymentsIconHeld()
					if len(i.aliases) != 2 || i.svg != paymentsSVG {
						return fmt.Errorf("Plotwork holds %+v, want two aliases and the same file", i)
					}
					return nil
				},
			},
			{
				Config: config(changed, `["payments service", "billing"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "content_sha256", sha256Hex(changed)),
					func(*terraform.State) error {
						if i, _ := api.paymentsIconHeld(); i.svg != changed {
							return fmt.Errorf("Plotwork holds %q, want the changed file", i.svg)
						}
						return nil
					},
				),
			},
			{
				Config:   config(changed, `["payments service", "billing"]`),
				PlanOnly: true,
			},
			{
				// Replaced in the portal: the next apply uploads the file again.
				PreConfig:          func() { api.touchIcon("payments-api", "<svg>other</svg>") },
				Config:             config(changed, `["payments service", "billing"]`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config(changed, `["payments service", "billing"]`),
				Check: func(*terraform.State) error {
					if i, _ := api.paymentsIconHeld(); i.svg != changed {
						return fmt.Errorf("Plotwork holds %q, want the configured file back", i.svg)
					}
					return nil
				},
			},
			{
				ResourceName:            "plotwork_organization_icon.payments",
				ImportState:             true,
				ImportStateId:           "acme/payments-api",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"svg", "content_sha256"},
			},
		},
	})
}

func TestIconAliasesTheAPICleansAreNoChange(t *testing.T) {
	api := newFakeAPI(t)
	config := api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name    = "payments-api"
  svg     = "<svg/>"
  aliases = ["  payments   service "]
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{Config: config, PlanOnly: true},
		},
	})
}

// iconOrigin is a TLS server that serves an icon file, which tests change.
type iconOrigin struct {
	srv         *httptest.Server
	mu          sync.Mutex
	body        string
	contentType string
}

// newIconOrigin starts an https origin and points the provider's downloads
// at a client that trusts it, until the test ends.
func newIconOrigin(t *testing.T, body string) *iconOrigin {
	t.Helper()
	o := &iconOrigin{body: body, contentType: "image/svg+xml"}
	o.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		o.mu.Lock()
		defer o.mu.Unlock()
		w.Header().Set("Content-Type", o.contentType)
		_, _ = w.Write([]byte(o.body))
	}))
	t.Cleanup(o.srv.Close)

	previous := sourceHTTPClient
	sourceHTTPClient = o.srv.Client()
	t.Cleanup(func() { sourceHTTPClient = previous })
	return o
}

// serve changes what the origin serves.
func (o *iconOrigin) serve(body, contentType string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.body, o.contentType = body, contentType
}

func TestIconFromSourceURLFollowsTheFile(t *testing.T) {
	api := newFakeAPI(t)
	origin := newIconOrigin(t, paymentsSVG)
	changed := strings.Replace(paymentsSVG, `r="10"`, `r="9"`, 1)
	config := api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name       = "payments-api"
  source_url = "` + origin.srv.URL + `/icons/payments-api.svg?sig=secret"
}`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("plotwork_organization_icon.payments", "content_sha256", sha256Hex(paymentsSVG)),
			},
			{Config: config, PlanOnly: true},
			{
				// A new file at the address is a planned change.
				PreConfig:          func() { origin.serve(changed, "application/octet-stream") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: func(*terraform.State) error {
					if i, _ := api.paymentsIconHeld(); i.svg != changed {
						return fmt.Errorf("Plotwork holds %q, want the new file", i.svg)
					}
					return nil
				},
			},
			{
				PreConfig:   func() { origin.serve("<html>sign in</html>", "text/html") },
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Could not download the icon.*not as an SVG`),
			},
		},
	})
}

func TestIconSourceURLRefusesWhatIsNotAnSVG(t *testing.T) {
	api := newFakeAPI(t)
	origin := newIconOrigin(t, strings.Repeat("<svg>", maxIconBytes))
	config := func(address string) string {
		return api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name       = "payments-api"
  source_url = "` + address + `"
}`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config("http://example.com/payments.svg"),
				ExpectError: regexp.MustCompile(`must be an https:// address`),
			},
			{
				Config:      config(origin.srv.URL + "/big.svg?sig=secret"),
				ExpectError: regexp.MustCompile(`larger than 32 KB`),
			},
			{
				// The signed query is never shown.
				PreConfig:   func() { origin.serve("{}", "application/json") },
				Config:      config(origin.srv.URL + "/big.svg?sig=secret"),
				ExpectError: regexp.MustCompile(`(?s)big.svg\?<query hidden> is served as\s+"application/json"`),
			},
		},
	})
}

func TestIconNeedsOneFile(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name = "payments-api"
}`,
			ExpectError: regexp.MustCompile(`No attribute specified when one \(and only one\) of`),
		}},
	})
}

func TestIconCheckIssuesAreShown(t *testing.T) {
	api := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name = "payments-api"
  svg  = "<svg><script>alert(1)</script></svg>"
}`,
				ExpectError: regexp.MustCompile(`(?s)icon.invalid.*unsafe: The SVG has a script Hint: Export it again without scripts`),
			},
			{
				Config: api.providerBlock(`organization = "acme"`) + `
resource "plotwork_organization_icon" "payments" {
  name = "Payments_API"
  svg  = "<svg/>"
}`,
				ExpectError: regexp.MustCompile(`lower-case letters and digits`),
			},
		},
	})
}

// patternFile is a pattern file as HCL, for jsonencode().
func patternFile(name, title, template string) string {
	return fmt.Sprintf(`{
    "$schema" = "https://docs.plotwork.dev/schemas/plotwork.pattern.json"
    name      = %q
    title     = %q
    summary   = "A payments service behind a gateway."
    templates = { neutral = %q }
    slots     = [{ id = "service", default = "Payments", meaning = "The service's name" }]
  }`, name, title, template)
}

func TestPatternLifecycle(t *testing.T) {
	api := newFakeAPI(t)
	config := func(content string) string {
		return api.providerBlock(`organization = "acme"`) + `
resource "plotwork_pattern" "payments" {
  content = ` + content + `
}`
	}
	source := "flow {{service}}"
	first := `jsonencode(` + patternFile("payments", "Payments", source) + `)`
	// The same file with other spacing and key order.
	reformatted := `<<-EOT
    {"templates": {"neutral": "` + source + `"}, "name": "payments",
     "summary": "A payments service behind a gateway.", "title": "Payments",
     "$schema": "https://docs.plotwork.dev/schemas/plotwork.pattern.json",
     "slots": [{"meaning": "The service's name", "id": "service", "default": "Payments"}]}
  EOT`
	retitled := `jsonencode(` + patternFile("payments", "Payments service", source) + `)`
	var savedAt string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := api.patternFieldsHeld("payments"); ok {
				return fmt.Errorf("destroy left the pattern")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "id", "acme/payments"),
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "name", "payments"),
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "pattern_id", "org:payments"),
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "title", "Payments"),
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "kind", "flow"),
					func(s *terraform.State) error {
						savedAt = s.RootModule().Resources["plotwork_pattern.payments"].Primary.Attributes["updated_at"]
						return nil
					},
				),
			},
			{Config: config(first), PlanOnly: true},
			{
				// Spacing and key order change nothing in Plotwork: no save.
				Config: config(reformatted),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "title", "Payments"),
					func(s *terraform.State) error {
						return resource.TestCheckResourceAttr("plotwork_pattern.payments", "updated_at", savedAt)(s)
					},
				),
			},
			{
				Config: config(retitled),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "title", "Payments service"),
					func(*terraform.State) error {
						held, _ := api.patternFieldsHeld("payments")
						if !strings.Contains(held, `"title":"Payments service"`) || strings.Contains(held, `"name"`) {
							return fmt.Errorf("Plotwork holds %s, want the new title and no name", held)
						}
						return nil
					},
				),
			},
			{
				// Changed in the portal: the plan shows it and the apply puts the file back.
				PreConfig:          func() { api.touchPattern("payments", "Edited") },
				Config:             config(retitled),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config(retitled),
				Check: func(*terraform.State) error {
					if held, _ := api.patternFieldsHeld("payments"); !strings.Contains(held, `"title":"Payments service"`) {
						return fmt.Errorf("Plotwork holds %s, want the file's title back", held)
					}
					return nil
				},
			},
			{
				ResourceName:            "plotwork_pattern.payments",
				ImportState:             true,
				ImportStateId:           "acme/payments",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"content"},
			},
		},
	})
}

func TestPatternRenamedIsReplaced(t *testing.T) {
	api := newFakeAPI(t)
	config := func(name string) string {
		return api.providerBlock(`organization = "acme"`) + `
resource "plotwork_pattern" "payments" {
  content = jsonencode(` + patternFile(name, "Payments", "flow") + `)
}`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("payments")},
			{
				Config: config("payments-service"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("plotwork_pattern.payments", "pattern_id", "org:payments-service"),
					func(*terraform.State) error {
						if _, ok := api.patternFieldsHeld("payments"); ok {
							return fmt.Errorf("the old pattern is still held")
						}
						if _, ok := api.patternFieldsHeld("payments-service"); !ok {
							return fmt.Errorf("the new pattern is not held")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestPatternErrorsAreExplained(t *testing.T) {
	api := newFakeAPI(t)
	api.patterns["acme"] = map[string]*fakePattern{"taken": {name: "taken", fields: map[string]any{"title": "T"}, updatedAt: 1}}
	config := func(content string) string {
		return api.providerBlock(`organization = "acme"`) + `
resource "plotwork_pattern" "p" {
  content = ` + content + `
}`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(`jsonencode(` + patternFile("taken", "Taken", "flow") + `)`),
				ExpectError: regexp.MustCompile(`(?s)orgs.pattern_exists.*terraform import`),
			},
			{
				Config:      config(`jsonencode(` + patternFile("payments", "Payments", `flow\nbroken`) + `)`),
				ExpectError: regexp.MustCompile(`(?s)orgs.pattern_invalid.*templates.neutral: E101 2:1 Unknown statement`),
			},
			{
				Config:      config(`jsonencode({ title = "No name" })`),
				ExpectError: regexp.MustCompile(`(?s)Not a pattern file.*has no name`),
			},
			{
				Config:      config(`jsonencode({ name = "payments", title = "No templates" })`),
				ExpectError: regexp.MustCompile(`(?s)Not a pattern file.*needs templates`),
			},
		},
	})
}

func TestPatternDeletedOutsideTerraformIsSavedAgain(t *testing.T) {
	api := newFakeAPI(t)
	config := api.providerBlock(`organization = "acme"`) + `
resource "plotwork_pattern" "payments" {
  content = jsonencode(` + patternFile("payments", "Payments", "flow") + `)
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.mu.Lock()
					delete(api.patterns["acme"], "payments")
					api.mu.Unlock()
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: config},
		},
	})
}
