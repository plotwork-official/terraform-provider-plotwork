// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

var (
	_ resource.Resource                = (*iconResource)(nil)
	_ resource.ResourceWithConfigure   = (*iconResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*iconResource)(nil)
	_ resource.ResourceWithImportState = (*iconResource)(nil)
)

// iconResource is plotwork_organization_icon: one of an organisation's
// custom icons, which its diagrams use as icon=org:<name>.
//
// Plotwork stores a checked, rewritten copy of the file, not the file, so
// the resource tracks what it sent by content_sha256. The hash is worked
// out during plan: from svg, or by downloading source_url. A file whose
// bytes change, here or at the address, is a planned change that uploads it
// again. An icon replaced outside Terraform (its updatedAt moved) is
// uploaded again on the next apply too.
type iconResource struct {
	data *providerData
}

// iconResourceModel is the plotwork_organization_icon block and its results.
type iconResourceModel struct {
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	SVG          types.String `tfsdk:"svg"`
	SourceURL    types.String `tfsdk:"source_url"`
	Aliases      types.List   `tfsdk:"aliases"`

	ID            types.String  `tfsdk:"id"`
	Ref           types.String  `tfsdk:"ref"`
	ContentSHA256 types.String  `tfsdk:"content_sha256"`
	MediaType     types.String  `tfsdk:"media_type"`
	Width         types.Float64 `tfsdk:"width"`
	Height        types.Float64 `tfsdk:"height"`
	Bytes         types.Int64   `tfsdk:"bytes"`
	UpdatedAt     types.String  `tfsdk:"updated_at"`
}

// iconNamePattern is the API's rule for an icon's name after "org:".
var iconNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// httpsPattern accepts an https address.
var httpsPattern = regexp.MustCompile(`^https://[^/?#\s]+`)

// The API's limits on an icon's names.
const (
	maxIconNameLength  = 48
	maxIconAliases     = 10
	maxIconAliasLength = 60
)

// iconFilePaths are the results a new file changes.
var iconFilePaths = []string{"media_type", "width", "height", "bytes", "updated_at"}

func newIconResource() resource.Resource {
	return &iconResource{}
}

// Metadata names the resource plotwork_organization_icon.
func (r *iconResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_icon"
}

// Schema describes plotwork_organization_icon.
func (r *iconResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One of an organisation's custom icons, which its diagrams draw with `icon=org:<name>` on the hosted service. " +
			"Give the SVG as text in `svg` (from `file()`, an `aws_s3_object` data source, or a `data \"http\"` download), " +
			"or as an https address in `source_url` that the provider downloads.\n\n" +
			"Plotwork rewrites the SVG from an allowlist and keeps that copy, so the provider tracks the file it sent by " +
			"`content_sha256`, worked out during plan. A change to the file, or to what `source_url` serves, uploads it again.\n\n" +
			"Needs the owner's plan to include custom icons (Pro or above), and the owner or an admin, or an organisation key " +
			"with the `" + designWriteScope + "` scope.",
		Attributes: map[string]schema.Attribute{
			"organization": organizationAttribute(),
			"name": schema.StringAttribute{
				MarkdownDescription: "The name after `org:`, such as `payments-api`: lower-case words joined by hyphens, at most 48 characters. " +
					"Changing it replaces the icon.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthAtMost(maxIconNameLength),
					stringvalidator.RegexMatches(iconNamePattern, "must be lower-case letters and digits in words joined by single hyphens, such as payments-api"),
				},
			},
			"svg": schema.StringAttribute{
				MarkdownDescription: "The SVG file's text, at most 32 KB, such as `file(\"${path.module}/icons/payments-api.svg\")` " +
					"or an `aws_s3_object` data source's `body` (which the aws provider fills only for a text type such as `application/xml`). " +
					"Set this or `source_url`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(path.MatchRoot("svg"), path.MatchRoot("source_url")),
				},
			},
			"source_url": schema.StringAttribute{
				MarkdownDescription: "An https address that serves the SVG, such as a CDN or a pre-signed object URL. The provider downloads it " +
					"during every plan (30 s timeout, at most 32 KB, `image/svg+xml` or an untyped body that is an SVG), so a new file there " +
					"is a planned change. Sensitive, because a signed address holds a credential. Set this or `svg`.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(httpsPattern, "must be an https:// address"),
				},
			},
			"aliases": schema.ListAttribute{
				MarkdownDescription: "Other names search and automatic icon matching find the icon by, such as `payments service`. At most 10.",
				Optional:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtMost(maxIconAliases),
					listvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, maxIconAliasLength)),
				},
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "`<organisation>/<name>`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"ref": schema.StringAttribute{
				MarkdownDescription: "What a diagram writes to draw the icon: `org:<name>`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"content_sha256": schema.StringAttribute{
				MarkdownDescription: "The hex SHA-256 of the file the provider sent. Empty after an import, or when the icon was replaced outside Terraform: the next apply uploads the file again.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"media_type": schema.StringAttribute{
				MarkdownDescription: "The stored file's type: `image/svg+xml`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"width": schema.Float64Attribute{
				MarkdownDescription: "The icon's width, from its view box.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
			"height": schema.Float64Attribute{
				MarkdownDescription: "The icon's height, from its view box.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
			"bytes": schema.Int64Attribute{
				MarkdownDescription: "The size of the stored, rewritten file.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "When the icon was last saved, RFC 3339 with milliseconds.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
		},
	}
}

// Configure keeps the provider's client.
func (r *iconResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan resolves the organisation and works out content_sha256 of the
// file to send, downloading source_url. A hash that differs from state, or
// changed aliases, mark the results the API sets unknown.
func (r *iconResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	planOrganization(ctx, r.data, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan iconResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hash := types.StringUnknown()
	if text, known := r.file(ctx, plan, &resp.Diagnostics); known {
		hash = types.StringValue(sha256Hex(text))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("content_sha256"), hash)...)

	if req.State.Raw.IsNull() {
		return
	}
	var state iconResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	switch {
	case !hash.Equal(state.ContentSHA256):
		for _, name := range iconFilePaths {
			resp.Diagnostics.Append(setUnknown(ctx, resp, name)...)
		}
	case !plan.Aliases.Equal(state.Aliases):
		resp.Diagnostics.Append(setUnknown(ctx, resp, "updated_at")...)
	}
}

// Create uploads the icon.
func (r *iconResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iconResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if r.upload(ctx, &plan, true, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Read refreshes the icon from the organisation's list, and removes it from
// state when the organisation no longer has it.
func (r *iconResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iconResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil {
		return
	}

	icon, err := r.data.client.GetIcon(ctx, state.Organization.ValueString(), state.Name.ValueString())
	if client.IsNotFound(err) || (err == nil && icon == nil) {
		tflog.Info(ctx, "The icon is gone from Plotwork; it will be uploaded again", map[string]any{"name": state.Name.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the organisation's icons", err)
		return
	}

	updatedAt := millisToRFC3339Milli(icon.UpdatedAt)
	if !state.UpdatedAt.IsNull() && state.UpdatedAt.ValueString() != updatedAt {
		// Saved by someone else: the file Plotwork holds is not known to be
		// the configured one, so the next apply uploads that again.
		tflog.Info(ctx, "The icon was changed outside Terraform", map[string]any{"name": state.Name.ValueString()})
		state.ContentSHA256 = types.StringValue("")
	}
	if !sameAliases(state.Aliases, icon.Aliases) {
		state.Aliases = stringList(icon.Aliases)
	}
	applyIcon(&state, icon)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update uploads the changed file, or sends only the aliases when the file
// is the one already uploaded.
func (r *iconResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state iconResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	withFile := plan.ContentSHA256.IsUnknown() || !plan.ContentSHA256.Equal(state.ContentSHA256)
	if r.upload(ctx, &plan, withFile, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete removes the icon. One that is already gone is not an error.
func (r *iconResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iconResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if err := r.data.client.DeleteIcon(ctx, state.Organization.ValueString(), state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Could not delete the icon", err)
	}
}

// ImportState adopts an icon by "<organisation>/<name>". Plotwork keeps a
// rewritten copy and not the file, so the next apply uploads the configured
// file once.
func (r *iconResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, name, ok := splitImportID(req.ID, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), org)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), org+"/"+name)...)
}

// file is the SVG text the plan sends, downloading source_url, and whether
// it is known yet. A download that fails adds an error.
func (r *iconResource) file(ctx context.Context, plan iconResourceModel, diags *diag.Diagnostics) (string, bool) {
	switch {
	case plan.SVG.IsUnknown() || plan.SourceURL.IsUnknown():
		return "", false
	case !plan.SVG.IsNull():
		return plan.SVG.ValueString(), true
	case !plan.SourceURL.IsNull():
		text, err := fetchIconSource(ctx, plan.SourceURL.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("source_url"), "Could not download the icon", err.Error())
			return "", false
		}
		return text, true
	}
	return "", false
}

// upload saves the icon, with the file when withFile is true, and copies the
// answer into plan. It reports whether it saved.
func (r *iconResource) upload(ctx context.Context, plan *iconResourceModel, withFile bool, diags *diag.Diagnostics) bool {
	request := client.IconRequest{Aliases: listStrings(ctx, plan.Aliases, diags)}
	if withFile {
		text, known := r.file(ctx, *plan, diags)
		if diags.HasError() || !known {
			return false
		}
		hash := sha256Hex(text)
		if !plan.ContentSHA256.IsUnknown() && plan.ContentSHA256.ValueString() != hash {
			diags.AddAttributeError(path.Root("source_url"), "The icon changed after the plan",
				"The file at source_url is not the one the plan read. Plan and apply again.")
			return false
		}
		request.SVG = text
		plan.ContentSHA256 = types.StringValue(hash)
	}

	org, name := plan.Organization.ValueString(), plan.Name.ValueString()
	saved, err := r.data.client.SaveIcon(ctx, org, name, request)
	if err != nil {
		addAPIError(diags, "Could not save the icon", err)
		return false
	}
	if len(saved.Removed) > 0 {
		diags.AddWarning("Plotwork left parts of the icon out",
			"The check removed what draws nothing: "+strings.Join(saved.Removed, ", ")+".")
	}
	plan.ID = types.StringValue(org + "/" + name)
	applyIcon(plan, &saved.Icon)
	return true
}

// applyIcon copies what the API says about an icon into the model.
func applyIcon(model *iconResourceModel, icon *client.Icon) {
	model.Ref = types.StringValue(icon.Ref)
	model.MediaType = types.StringValue(icon.MediaType)
	model.Width = types.Float64Value(icon.Width)
	model.Height = types.Float64Value(icon.Height)
	model.Bytes = types.Int64Value(icon.Bytes)
	model.UpdatedAt = types.StringValue(millisToRFC3339Milli(icon.UpdatedAt))
	if model.ContentSHA256.IsNull() || model.ContentSHA256.IsUnknown() {
		model.ContentSHA256 = types.StringValue("")
	}
	if model.ID.IsNull() || model.ID.IsUnknown() {
		model.ID = types.StringValue(model.Organization.ValueString() + "/" + model.Name.ValueString())
	}
}

// sameAliases reports whether the configured aliases are the ones the API
// holds, after the API's own clean-up: trimmed, inner spaces joined, the
// first of each repeat kept. Null and empty are the same.
func sameAliases(configured types.List, held []string) bool {
	var want []string
	for _, v := range configured.Elements() {
		s, ok := v.(types.String)
		if !ok {
			return false
		}
		alias := strings.Join(strings.Fields(s.ValueString()), " ")
		if !slices.Contains(want, alias) {
			want = append(want, alias)
		}
	}
	return slices.Equal(want, held) || (len(want) == 0 && len(held) == 0)
}

// listStrings is a Terraform list of strings as Go, nil when null.
func listStrings(ctx context.Context, list types.List, diags *diag.Diagnostics) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(list.ElementsAs(ctx, &out, false)...)
	return out
}

// setUnknown marks one planned string, number or float result unknown.
func setUnknown(ctx context.Context, resp *resource.ModifyPlanResponse, name string) diag.Diagnostics {
	switch name {
	case "bytes":
		return resp.Plan.SetAttribute(ctx, path.Root(name), types.Int64Unknown())
	case "width", "height":
		return resp.Plan.SetAttribute(ctx, path.Root(name), types.Float64Unknown())
	default:
		return resp.Plan.SetAttribute(ctx, path.Root(name), types.StringUnknown())
	}
}
