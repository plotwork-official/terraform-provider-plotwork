// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

// API error codes the provider explains with more than the API's message.
const (
	codeRESTNotIncluded = "plan.rest_not_included"
	codeDiagramInvalid  = "diagram.invalid"
	codeDiagramLocked   = "hosting.diagram_locked"
	codeKeyWrongOrg     = "orgs.key_wrong_org"
)

// providerDataFrom reads what Configure handed over. It returns nil without
// a diagnostic while the provider is not configured yet, which the framework
// does during validation, and adds an error for anything else.
func providerDataFrom(data any, diags *diag.Diagnostics) *providerData {
	if data == nil {
		return nil
	}
	pd, ok := data.(*providerData)
	if !ok {
		diags.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *providerData, got %T. This is a bug in the provider; please report it.", data))
		return nil
	}
	return pd
}

// requireAPIKey adds an error and returns false when no API key was set.
func (pd *providerData) requireAPIKey(diags *diag.Diagnostics) bool {
	if pd.hasAPIKey {
		return true
	}
	diags.AddError("Missing Plotwork API key",
		"Set api_key in the provider block or the "+envAPIKey+" environment variable. "+
			"Create a key in the Plotwork portal under API keys, or an organisation key under Keys and CI.")
	return false
}

// addAPIError adds an error for a failed API call, with what to do about the
// failures a Terraform user can fix.
func addAPIError(diags *diag.Diagnostics, summary string, err error) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		diags.AddError(summary, err.Error())
		return
	}

	detail := apiErr.Error()
	switch {
	case apiErr.StatusCode == http.StatusUnauthorized:
		detail += "\n\nThe API key is missing, revoked or expired. Check api_key or " + envAPIKey + "."
	case apiErr.Code == codeRESTNotIncluded:
		detail += "\n\nDrawing over the REST API (plotwork_import, plotwork_render) needs the Pro plan or above. " +
			"plotwork_diagram publishes on every plan."
	case apiErr.Code == codeKeyWrongOrg:
		detail += "\n\nThis is an organisation key. Set organization on the provider or the resource to publish to its library."
	case apiErr.Code == codeDiagramLocked:
		detail += "\n\nThe diagram is locked in Plotwork. Unlock it in the portal, then apply again."
	case apiErr.Code == codeDiagramInvalid:
		if lines := diagnosticLines(apiErr.Details); len(lines) > 0 {
			detail += "\n\n" + strings.Join(lines, "\n")
		}
	default:
		if hint := designHint(apiErr); hint != "" {
			detail += "\n\n" + hint
		}
	}
	diags.AddError(summary, detail)
}

// diagnosticLines formats the diagnostics in a 422 answer's details, one a
// line. Details that are not a list of diagnostics give no lines.
func diagnosticLines(details json.RawMessage) []string {
	var found []client.Diagnostic
	if len(details) == 0 || json.Unmarshal(details, &found) != nil {
		return nil
	}
	return formatDiagnostics(found)
}

// formatDiagnostics formats each diagnostic as one line.
func formatDiagnostics(found []client.Diagnostic) []string {
	lines := make([]string, 0, len(found))
	for _, d := range found {
		lines = append(lines, d.String())
	}
	return lines
}

// stringList converts Go strings to a Terraform list, empty rather than null
// so that length() works on it.
func stringList(values []string) types.List {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

// contentID is a stable id for content: the hex SHA-256 of its parts.
func contentID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		// The length prefix keeps ("ab", "c") apart from ("a", "bc").
		fmt.Fprintf(h, "%d:%s;", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// optionalString is a Terraform string as Go: empty when null or unknown.
func optionalString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// nullableString converts an API value that may be null.
func nullableString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}
