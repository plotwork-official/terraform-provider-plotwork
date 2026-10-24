// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

var (
	_ resource.Resource                = (*patternResource)(nil)
	_ resource.ResourceWithConfigure   = (*patternResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*patternResource)(nil)
	_ resource.ResourceWithImportState = (*patternResource)(nil)
)

// patternResource is plotwork_pattern: one of an organisation's approved
// patterns, saved from a pattern file (<name>.pattern.json).
//
// The file is kept in state as configured and compared as JSON, so spacing
// and key order are not a change. The name inside it is the pattern's name
// for good: a new name replaces the pattern. A pattern changed outside
// Terraform (its updatedAt moved) reads back as Plotwork holds it, so the
// plan shows the difference and the next apply puts the file back.
type patternResource struct {
	data *providerData
}

// patternResourceModel is the plotwork_pattern block and its results.
type patternResourceModel struct {
	Organization types.String     `tfsdk:"organization"`
	Content      normalisedString `tfsdk:"content"`

	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	PatternID types.String `tfsdk:"pattern_id"`
	Title     types.String `tfsdk:"title"`
	Kind      types.String `tfsdk:"kind"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

// maxPatternNameLength is the API's limit on a pattern's name.
const maxPatternNameLength = 50

// patternResultPaths are the results a changed file changes.
var patternResultPaths = []string{"title", "kind", "updated_at"}

func newPatternResource() resource.Resource {
	return &patternResource{}
}

// Metadata names the resource plotwork_pattern.
func (r *patternResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pattern"
}

// Schema describes plotwork_pattern.
func (r *patternResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One of an organisation's approved patterns: a diagram with fixed parts every team draws the same, " +
			"and `{{slot}}` markers for what each team fills in. Plotwork's agents offer the organisation's patterns first.\n\n" +
			"`content` is a pattern file, such as `file(\"${path.module}/payments.pattern.json\")`. Its format is described by " +
			"[the pattern schema](https://docs.plotwork.dev/schemas/plotwork.pattern.json); the Plotwork MCP server's `get_pattern` " +
			"with `file: true` gives one to start from, and `check_pattern` checks it. Plotwork checks every template before it saves.\n\n" +
			"Needs the Enterprise plan, and the owner or an admin, or an organisation key with the `" + designWriteScope + "` scope.",
		Attributes: map[string]schema.Attribute{
			"organization": organizationAttribute(),
			"content": schema.StringAttribute{
				MarkdownDescription: "The pattern file as JSON text. It is compared as JSON, so spacing and key order are not a change. " +
					"Its `name` is the pattern's name: changing it replaces the pattern.",
				Required:   true,
				CustomType: normalisedStringType{n: normaliseJSON},
				Validators: []validator.String{patternFileValidator{}},
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "`<organisation>/<name>`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The pattern's name, read from `content`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"pattern_id": schema.StringAttribute{
				MarkdownDescription: "What the tools take to draw from it: `org:<name>`, such as in `draw_from_pattern`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The pattern's title, as saved.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "The kind of diagram its templates draw, such as `flow`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "When the pattern was last saved, RFC 3339 with milliseconds.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
		},
	}
}

// Configure keeps the provider's client.
func (r *patternResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan resolves the organisation, reads the name from the file, asks
// for a replacement when the name changes, and marks the results unknown
// when the file does.
func (r *patternResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	planOrganization(ctx, r.data, req, resp)
	var plan patternResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := types.StringUnknown()
	if !plan.Content.IsUnknown() {
		if n, err := patternName(plan.Content.ValueString()); err == nil {
			name = types.StringValue(n)
		}
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("name"), name)...)

	if req.State.Raw.IsNull() {
		return
	}
	var state patternResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !name.Equal(state.Name) {
		if !name.IsUnknown() {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("name"))
		}
		for _, p := range []string{"id", "pattern_id"} {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(p), types.StringUnknown())...)
		}
	}
	if !plan.Content.sameAs(state.Content) {
		for _, p := range patternResultPaths {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(p), types.StringUnknown())...)
		}
	}
}

// Create saves the pattern.
func (r *patternResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan patternResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	saved, err := r.data.client.CreatePattern(ctx, plan.Organization.ValueString(), json.RawMessage(plan.Content.ValueString()))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not save the pattern", err)
		return
	}
	applyPattern(&plan, saved)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the pattern, and removes it from state when the
// organisation no longer has it.
func (r *patternResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state patternResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil {
		return
	}

	got, err := r.data.client.GetPattern(ctx, state.Organization.ValueString(), state.Name.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, "The pattern is gone from Plotwork; it will be saved again", map[string]any{"name": state.Name.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the pattern", err)
		return
	}

	updatedAt := millisToRFC3339Milli(got.UpdatedAt)
	if state.Content.IsNull() || state.UpdatedAt.ValueString() != updatedAt {
		// Imported, or saved by someone else: state takes what Plotwork
		// holds, so the plan shows how it differs from the file.
		text, err := patternFileOf(got)
		if err != nil {
			resp.Diagnostics.AddError("Could not read the pattern", err.Error())
			return
		}
		state.Content = jsonValue(text)
	}
	applyPattern(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update replaces the pattern's fields, keeping its name. A change of
// spacing or key order alone sends nothing.
func (r *patternResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state patternResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	org := plan.Organization.ValueString()
	name, err := patternName(plan.Content.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("content"), "Not a pattern file", err.Error())
		return
	}

	var saved *client.Pattern
	switch {
	case name != state.Name.ValueString():
		// The name was unknown during plan, so no replacement was planned:
		// save the new pattern, then remove the old one.
		saved, err = r.data.client.CreatePattern(ctx, org, json.RawMessage(plan.Content.ValueString()))
		if err == nil {
			if delErr := r.data.client.DeletePattern(ctx, state.Organization.ValueString(), state.Name.ValueString()); delErr != nil && !client.IsNotFound(delErr) {
				resp.Diagnostics.AddWarning("Could not remove the pattern this one replaced", delErr.Error())
			}
		}
	case plan.Content.sameAs(state.Content) && !state.UpdatedAt.IsNull():
		plan.Title, plan.Kind, plan.UpdatedAt = state.Title, state.Kind, state.UpdatedAt
		plan.ID, plan.PatternID = state.ID, state.PatternID
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	default:
		fields, ferr := patternFields(plan.Content.ValueString())
		if ferr != nil {
			resp.Diagnostics.AddAttributeError(path.Root("content"), "Not a pattern file", ferr.Error())
			return
		}
		saved, err = r.data.client.UpdatePattern(ctx, org, name, fields)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not save the pattern", err)
		return
	}
	applyPattern(&plan, saved)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the pattern. One that is already gone is not an error.
func (r *patternResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state patternResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if err := r.data.client.DeletePattern(ctx, state.Organization.ValueString(), state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Could not delete the pattern", err)
	}
}

// ImportState adopts a pattern by "<organisation>/<name>". Its content is
// read as Plotwork holds it, so the next apply saves the configured file
// over it once if the two differ.
func (r *patternResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, name, ok := splitImportID(req.ID, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), org)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), org+"/"+name)...)
}

// applyPattern copies the API's answer into the model.
func applyPattern(model *patternResourceModel, p *client.Pattern) {
	model.Name = types.StringValue(p.Name)
	model.ID = types.StringValue(model.Organization.ValueString() + "/" + p.Name)
	model.PatternID = types.StringValue(p.ID)
	model.Title = types.StringValue(p.Title)
	model.Kind = types.StringValue(p.Kind)
	model.UpdatedAt = types.StringValue(millisToRFC3339Milli(p.UpdatedAt))
}

// patternName reads the name from a pattern file.
func patternName(content string) (string, error) {
	var file struct {
		Name *string `json:"name"`
	}
	if err := json.Unmarshal([]byte(content), &file); err != nil {
		return "", fmt.Errorf("the pattern file is not a JSON object: %w", err)
	}
	if file.Name == nil {
		return "", fmt.Errorf("the pattern file has no name")
	}
	name := *file.Name
	if len(name) > maxPatternNameLength || !iconNamePattern.MatchString(name) {
		return "", fmt.Errorf("the name %q must be lower-case letters and digits in words joined by single hyphens, at most %d characters, such as payments-service", name, maxPatternNameLength)
	}
	return name, nil
}

// patternFields is a pattern file without its name and $schema: the body of
// a replace.
func patternFields(content string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &fields); err != nil {
		return nil, fmt.Errorf("the pattern file is not a JSON object: %w", err)
	}
	delete(fields, "name")
	delete(fields, "$schema")
	return json.Marshal(fields)
}

// patternFileOf writes a stored pattern back as a pattern file.
func patternFileOf(p *client.Pattern) (string, error) {
	file := map[string]any{
		"name": p.Name, "title": p.Title, "summary": p.Summary, "whenToUse": p.WhenToUse,
		"tags": p.Tags, "templates": p.Templates, "slots": p.Slots, "notes": p.Notes,
	}
	out, err := json.MarshalIndent(file, "", "  ")
	return string(out), err
}

// patternFileValidator checks that content is a JSON object with a name and
// templates, so a mistake shows during plan, not as the API's 422.
type patternFileValidator struct{}

// Description says what the validator checks.
func (patternFileValidator) Description(_ context.Context) string {
	return "must be a pattern file: a JSON object with a name and templates"
}

// MarkdownDescription says what the validator checks.
func (v patternFileValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// ValidateString checks a known value.
func (patternFileValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	content := req.ConfigValue.ValueString()
	if _, err := patternName(content); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Not a pattern file", err.Error())
		return
	}
	var file struct {
		Templates map[string]string `json:"templates"`
	}
	if err := json.Unmarshal([]byte(content), &file); err != nil || len(file.Templates) == 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Not a pattern file",
			"The pattern file needs templates: the Plotwork source of at least one flavour (neutral, aws, azure, gcp).")
	}
}
