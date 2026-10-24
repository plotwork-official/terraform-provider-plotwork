// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

var (
	_ resource.Resource                = (*brandResource)(nil)
	_ resource.ResourceWithConfigure   = (*brandResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*brandResource)(nil)
	_ resource.ResourceWithImportState = (*brandResource)(nil)
)

// brandResource is plotwork_organization_brand: the colours an
// organisation's diagrams are painted in on the hosted service.
//
// An organisation has one brand, so the resource's id is the organisation.
// The colours are compared as lower-case #rrggbb, so #2A78D6 and #2a78d6
// are the same brand and neither shows as a change.
type brandResource struct {
	data *providerData
}

// brandResourceModel is the plotwork_organization_brand block and its
// results.
type brandResourceModel struct {
	Organization types.String     `tfsdk:"organization"`
	Primary      normalisedString `tfsdk:"primary"`
	Secondary    normalisedString `tfsdk:"secondary"`
	Accent       normalisedString `tfsdk:"accent"`

	ID                 types.String `tfsdk:"id"`
	EffectivePrimary   types.String `tfsdk:"effective_primary"`
	EffectiveSecondary types.String `tfsdk:"effective_secondary"`
	EffectiveAccent    types.String `tfsdk:"effective_accent"`
	Active             types.Bool   `tfsdk:"active"`
}

// The brand roles a person may choose besides the primary, as the API names
// them in chosen.
const (
	roleSecondary = "secondary"
	roleAccent    = "accent"
)

func newBrandResource() resource.Resource {
	return &brandResource{}
}

// Metadata names the resource plotwork_organization_brand.
func (r *brandResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_brand"
}

// Schema describes plotwork_organization_brand.
func (r *brandResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	colour := func(description string, required bool) schema.StringAttribute {
		return schema.StringAttribute{
			MarkdownDescription: description,
			Required:            required,
			Optional:            !required,
			CustomType:          normalisedStringType{n: normaliseColour},
			Validators: []validator.String{
				stringvalidator.RegexMatches(hexColour, "must be a colour written #rrggbb or #rgb, such as #2a78d6"),
			},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "An organisation's brand theme: the colours its diagrams are painted in on the hosted service, " +
			"in place of Plotwork's blue, violet and aqua. An organisation has one brand; destroying the resource removes it.\n\n" +
			"Needs the owner's plan to include brand themes (Pro or above), and the owner or an admin, or an organisation key " +
			"with the `" + designWriteScope + "` scope.",
		Attributes: map[string]schema.Attribute{
			"organization": organizationAttribute(),
			"primary": colour("The main brand colour, `#rrggbb` or `#rgb` in any case. It takes the place of the blue tone. "+
				"Case and the short form are not a change.", true),
			"secondary": colour("Takes the place of the violet tone. Left out, Plotwork makes it from `primary` and it follows `primary`; "+
				"`effective_secondary` says which colour that is.", false),
			"accent": colour("Takes the place of the aqua tone. Left out, Plotwork makes it from `primary` and it follows `primary`; "+
				"`effective_accent` says which colour that is.", false),

			"id": schema.StringAttribute{
				MarkdownDescription: "The organisation, as configured: an organisation has one brand.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"effective_primary": schema.StringAttribute{
				MarkdownDescription: "The primary colour as Plotwork stores it, lower-case `#rrggbb`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"effective_secondary": schema.StringAttribute{
				MarkdownDescription: "The secondary colour Plotwork paints with: `secondary`, or the one made from `primary`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"effective_accent": schema.StringAttribute{
				MarkdownDescription: "The accent colour Plotwork paints with: `accent`, or the one made from `primary`.",
				Computed:            true,
				PlanModifiers:       keepString(),
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the hosted service paints with the brand. False when the owner's plan no longer includes brand themes; the brand is kept.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// Configure keeps the provider's client.
func (r *brandResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan resolves the organisation, and marks the colours Plotwork works
// out unknown when a configured colour changes.
func (r *brandResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	planOrganization(ctx, r.data, req, resp)
	if req.State.Raw.IsNull() || resp.Diagnostics.HasError() {
		return
	}

	var plan, state brandResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Primary.sameAs(state.Primary) && plan.Secondary.sameAs(state.Secondary) && plan.Accent.sameAs(state.Accent) {
		return
	}
	for _, name := range []string{"effective_primary", "effective_secondary", "effective_accent"} {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(name), types.StringUnknown())...)
	}
}

// Create saves the brand.
func (r *brandResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan brandResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if r.save(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Read refreshes the brand, and removes it from state when the organisation
// has none, so the next apply saves it again.
func (r *brandResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state brandResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil {
		return
	}

	got, err := r.data.client.GetBrand(ctx, state.Organization.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the organisation's brand", err)
		return
	}
	if got.Brand == nil {
		tflog.Info(ctx, "The organisation has no brand; it will be saved again", map[string]any{"organization": state.Organization.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	// A colour Plotwork makes from the primary was not configured: it stays
	// null, so a configuration that leaves it out shows no change.
	state.Primary = colourValue(got.Brand.Primary)
	state.Secondary, state.Accent = colourNull(), colourNull()
	if slices.Contains(got.Chosen, roleSecondary) {
		state.Secondary = colourValue(got.Brand.Secondary)
	}
	if slices.Contains(got.Chosen, roleAccent) {
		state.Accent = colourValue(got.Brand.Accent)
	}
	state.ID = state.Organization
	applyBrandColours(&state, *got.Brand)
	state.Active = types.BoolValue(got.Active)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update saves the changed colours.
func (r *brandResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan brandResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if r.save(ctx, &plan, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete removes the brand. The API answers 204 also when there is none.
func (r *brandResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state brandResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if err := r.data.client.DeleteBrand(ctx, state.Organization.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Could not remove the organisation's brand", err)
	}
}

// ImportState adopts an organisation's brand by the organisation's slug or
// id.
func (r *brandResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// save sends the planned colours and copies the answer into plan. The
// check's warnings become Terraform warnings. It reports whether it saved.
func (r *brandResource) save(ctx context.Context, plan *brandResourceModel, diags *diag.Diagnostics) bool {
	request := client.BrandRequest{Primary: plan.Primary.normalised()}
	if s := plan.Secondary.normalised(); s != "" {
		request.Secondary = &s
	}
	if a := plan.Accent.normalised(); a != "" {
		request.Accent = &a
	}

	saved, err := r.data.client.SaveBrand(ctx, plan.Organization.ValueString(), request)
	if err != nil {
		addAPIError(diags, "Could not save the organisation's brand", err)
		return false
	}
	for _, issue := range saved.Check.Issues {
		if issue.Severity == "warning" {
			diags.AddWarning("Plotwork's check of the brand: "+issue.Role, issue.Message)
		}
	}
	plan.ID = plan.Organization
	applyBrandColours(plan, saved.Brand)
	// Saving succeeds only on a plan with brand themes, so it paints now.
	plan.Active = types.BoolValue(true)
	return true
}

// applyBrandColours copies the colours Plotwork paints with into the model.
func applyBrandColours(model *brandResourceModel, colours client.BrandColors) {
	model.EffectivePrimary = types.StringValue(colours.Primary)
	model.EffectiveSecondary = types.StringValue(colours.Secondary)
	model.EffectiveAccent = types.StringValue(colours.Accent)
}
