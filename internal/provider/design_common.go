// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

// The organisation's design resources (plotwork_organization_brand,
// plotwork_organization_icon, plotwork_pattern) share the organisation
// argument, the error hints and the import id handled here.

// API error codes of the design routes the provider explains.
const (
	codeOrgsForbidden         = "orgs.forbidden"
	codeBrandNotIncluded      = "plan.brand_not_included"
	codeIconsNotIncluded      = "plan.icons_not_included"
	codeGovernanceNotIncluded = "plan.governance_not_included"
	codeIconLimitReached      = "orgs.icon_limit_reached"
	codePatternLimitReached   = "orgs.pattern_limit_reached"
	codePatternExists         = "orgs.pattern_exists"
	codeBrandInvalid          = "brand.invalid"
	codeIconInvalid           = "icon.invalid"
	codePatternInvalid        = "orgs.pattern_invalid"
)

// designWriteScope is the scope an organisation key needs for the design
// routes.
const designWriteScope = "design:write"

// organizationAttributeDescr describes every design resource's organization.
const organizationAttributeDescr = "The slug or id of the organisation. Defaults to the provider's `organization` (or `" + envOrganization + "`). " +
	"Changing it, or the provider's value when this is left out, replaces the resource in the new organisation."

// organizationAttribute is the organization argument of a design resource.
// It is computed so that, left out, it takes the provider's value; the
// resource's ModifyPlan resolves it and asks for a replacement on a change.
func organizationAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: organizationAttributeDescr,
		Optional:            true,
		Computed:            true,
	}
}

// keepString keeps a computed string from state while nothing changes it.
func keepString() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
}

// planOrganization sets the planned organization to the configured one, or
// the provider's, and asks for a replacement when it differs from state.
// It adds an error when neither names one.
func planOrganization(ctx context.Context, data *providerData, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var configured types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("organization"), &configured)...)
	if resp.Diagnostics.HasError() || configured.IsUnknown() {
		return
	}
	org := configured.ValueString()
	if configured.IsNull() && data != nil {
		org = data.organization
	}
	if org == "" {
		if data != nil {
			resp.Diagnostics.AddAttributeError(path.Root("organization"), "No organisation",
				"Set organization on the resource, or on the provider (or the "+envOrganization+" environment variable).")
		}
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("organization"), org)...)

	if req.State.Raw.IsNull() {
		return
	}
	var current types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization"), &current)...)
	if current.ValueString() != org {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("organization"))
	}
}

// splitImportID reads an import id of the form "<organisation>/<name>".
func splitImportID(id string, diags *diag.Diagnostics) (org, name string, ok bool) {
	org, name, found := cutLast(id, "/")
	if !found || org == "" || name == "" {
		diags.AddError("Unexpected import id",
			fmt.Sprintf("Expected <organisation>/<name>, such as acme/payments-api, got %q.", id))
		return "", "", false
	}
	return org, name, true
}

// designHint is what a Terraform user can do about an error of the design
// routes, or "" for an error the API's message explains well enough.
func designHint(apiErr *client.APIError) string {
	switch apiErr.Code {
	case codeOrgsForbidden:
		return "Changing an organisation's brand, icons or patterns needs the owner or an admin. An organisation key needs the " +
			designWriteScope + " scope, which the owner or an admin gives it under Keys and CI."
	case codeBrandNotIncluded:
		return "Brand themes need the organisation owner's plan to include them (Pro or above)."
	case codeIconsNotIncluded:
		return "Custom icons need the organisation owner's plan to include them (Pro or above)."
	case codeGovernanceNotIncluded:
		return "An organisation's own patterns are part of the Enterprise plan."
	case codeIconLimitReached:
		return "The organisation keeps as many icons as its plan allows. Remove one, or upgrade the plan."
	case codePatternLimitReached:
		return "The organisation keeps as many patterns as it may. Remove one before adding another."
	case codePatternExists:
		return "Adopt the existing pattern with terraform import, as <organisation>/<name>, or choose another name in the file."
	case codeBrandInvalid:
		return strings.Join(brandIssueLines(apiErr.Details), "\n")
	case codeIconInvalid:
		return strings.Join(iconIssueLines(apiErr.Details), "\n")
	case codePatternInvalid:
		return strings.Join(patternProblemLines(apiErr.Details), "\n")
	}
	return ""
}

// brandIssueLines formats brand.invalid details, one issue a line.
func brandIssueLines(details json.RawMessage) []string {
	var issues []client.BrandIssue
	if len(details) == 0 || json.Unmarshal(details, &issues) != nil {
		return nil
	}
	lines := make([]string, 0, len(issues))
	for _, i := range issues {
		lines = append(lines, fmt.Sprintf("%s %s (%s): %s", i.Severity, i.Role, i.Code, i.Message))
	}
	return lines
}

// iconIssueLines formats icon.invalid details, one issue a line.
func iconIssueLines(details json.RawMessage) []string {
	var issues []client.IconIssue
	if len(details) == 0 || json.Unmarshal(details, &issues) != nil {
		return nil
	}
	lines := make([]string, 0, len(issues))
	for _, i := range issues {
		line := i.Code + ": " + i.Message
		if i.Hint != "" {
			line += " Hint: " + i.Hint
		}
		lines = append(lines, line)
	}
	return lines
}

// patternProblemLines formats orgs.pattern_invalid details, which are
// {"problems": [...]}, one problem a line.
func patternProblemLines(details json.RawMessage) []string {
	var found struct {
		Problems []client.PatternProblem `json:"problems"`
	}
	if len(details) == 0 || json.Unmarshal(details, &found) != nil {
		return nil
	}
	lines := make([]string, 0, len(found.Problems))
	for _, p := range found.Problems {
		lines = append(lines, p.Field+": "+p.Message)
	}
	return lines
}

// millisToRFC3339Milli formats milliseconds since the epoch in UTC, keeping
// the milliseconds, so two saves in one second still differ.
func millisToRFC3339Milli(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
