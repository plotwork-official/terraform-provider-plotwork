// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

var (
	_ resource.Resource                = (*diagramResource)(nil)
	_ resource.ResourceWithConfigure   = (*diagramResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*diagramResource)(nil)
	_ resource.ResourceWithImportState = (*diagramResource)(nil)
)

// diagramResource is plotwork_diagram: a diagram published to a person's
// library, with a public link, or to an organisation's private library.
//
// Plotwork stores a diagram by its content, so a change to the source is a
// new diagram with a new id and link. An update publishes the new diagram
// first and then deletes the one it replaced, so a viewer never meets a
// missing picture, and the stable link (alias) moves to the new one.
type diagramResource struct {
	data *providerData
}

// diagramResourceModel is the plotwork_diagram block and its results.
type diagramResourceModel struct {
	Source         types.String `tfsdk:"source"`
	Name           types.String `tfsdk:"name"`
	Folder         types.String `tfsdk:"folder"`
	Scheme         types.String `tfsdk:"scheme"`
	EmbedSource    types.Bool   `tfsdk:"embed_source"`
	Alias          types.String `tfsdk:"alias"`
	Organization   types.String `tfsdk:"organization"`
	Classification types.String `tfsdk:"classification"`

	ID          types.String `tfsdk:"id"`
	URL         types.String `tfsdk:"url"`
	ViewURL     types.String `tfsdk:"view_url"`
	StableURL   types.String `tfsdk:"stable_url"`
	Library     types.String `tfsdk:"library"`
	Bytes       types.Int64  `tfsdk:"bytes"`
	PublishedAt types.String `tfsdk:"published_at"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
}

// contentPaths are the arguments that change what is published. A change to
// any of them republishes; the results then come from the new answer.
var contentPaths = []string{"source", "name", "folder", "scheme", "embed_source", "alias", "classification"}

// resultPaths are the results a republish can change. stable_url is not one:
// it stays the same while the alias does.
var resultPaths = []string{"id", "url", "view_url", "library", "bytes", "published_at", "expires_at"}

func newDiagramResource() resource.Resource {
	return &diagramResource{}
}

// Metadata names the resource plotwork_diagram.
func (r *diagramResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_diagram"
}

// Schema describes plotwork_diagram.
func (r *diagramResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Publishes a diagram to Plotwork, so the picture changes when the infrastructure does. " +
			"Without `organization` it goes to the API key's own library and gets a public link; with one, or with an " +
			"organisation key, it goes to that organisation's private library.\n\n" +
			"A change to the source publishes a new diagram and then deletes the one it replaced. " +
			"Set `alias` for a `stable_url` that never changes, to embed in a README or a wiki. " +
			"Publishing works on every plan.",
		Attributes: map[string]schema.Attribute{
			"source": schema.StringAttribute{
				MarkdownDescription: "The Plotwork source to publish: from `plotwork_import`, a `.diagram` file, or `templatefile()`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "A label for the diagram in the library. Removing it later keeps the old label.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 80)},
			},
			"folder": schema.StringAttribute{
				MarkdownDescription: "The folder to file it in, outermost first, such as `Platform/Network`. Left out, the top level.",
				Optional:            true,
			},
			"scheme": schema.StringAttribute{
				MarkdownDescription: "`light` (the default), `dark`, or `auto` for one picture that follows the viewer's setting.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("light"),
				Validators:          []validator.String{stringvalidator.OneOf(schemes...)},
			},
			"embed_source": schema.BoolAttribute{
				MarkdownDescription: "Write the source into the stored SVG so the diagram can be opened in the Plotwork editor. " +
					"Anyone who can open the picture can then read the source. Left out, a personal diagram leaves it out and " +
					"an organisation's follows its \"Keep diagrams editable\" setting.",
				Optional: true,
			},
			"alias": schema.StringAttribute{
				MarkdownDescription: "The name of a stable link, such as `prod-network`: lower-case words joined by hyphens. " +
					"`stable_url` then always shows the latest diagram. Personal libraries only; the plan limits how many an account keeps.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(60),
					stringvalidator.RegexMatches(aliasPattern, "must be lower-case letters and digits in words joined by single hyphens, such as prod-network"),
					stringvalidator.ConflictsWith(path.MatchRoot("organization")),
				},
			},
			"organization": schema.StringAttribute{
				MarkdownDescription: "The slug or id of the organisation whose private library holds the diagram. " +
					"Defaults to the provider's `organization`. Changing it publishes to the new library and deletes the old copy.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"classification": schema.StringAttribute{
				MarkdownDescription: "For an organisation's diagram: `public`, `internal` or `confidential`. " +
					"Left out, the folder's or the organisation's default.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("public", "internal", "confidential")},
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "The diagram's id in Plotwork.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "The public address of the SVG, for a personal diagram. Null for an organisation's, which has none.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"view_url": schema.StringAttribute{
				MarkdownDescription: "The portal page that shows an organisation's diagram to its signed-in members.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"stable_url": schema.StringAttribute{
				MarkdownDescription: "The address of the stable link named by `alias`. It never changes while the alias stays.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"library": schema.StringAttribute{
				MarkdownDescription: "Where the diagram is stored: `personal`, or the id of the organisation whose library holds it. " +
					"An organisation's Publishing setting can send a personal publish to its library.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"bytes": schema.Int64Attribute{
				MarkdownDescription: "The size of the stored SVG.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"published_at": schema.StringAttribute{
				MarkdownDescription: "When it was last published, RFC 3339.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "When the plan's retention deletes it, RFC 3339, or null when the plan keeps it. " +
					"Each apply that publishes moves it out; a diagram that expired is published again by the next apply.",
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

// Configure keeps the provider's client.
func (r *diagramResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan marks the results unknown when the content changes, because the
// API answers a changed diagram with a new id, link and time. Without a
// change every result keeps its value from state (UseStateForUnknown).
func (r *diagramResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // A create or a destroy: the framework plans those itself.
	}

	changed := false
	for _, name := range contentPaths {
		var planned, current types.String
		var plannedBool, currentBool types.Bool
		p := path.Root(name)
		if name == "embed_source" {
			resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, p, &plannedBool)...)
			resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &currentBool)...)
			changed = changed || !plannedBool.Equal(currentBool)
			continue
		}
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, p, &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &current)...)
		changed = changed || !planned.Equal(current)
		if name == "alias" && !planned.Equal(current) {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("stable_url"), types.StringUnknown())...)
		}
	}
	if !changed || resp.Diagnostics.HasError() {
		return
	}
	for _, name := range resultPaths {
		if name == "bytes" {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(name), types.Int64Unknown())...)
			continue
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(name), types.StringUnknown())...)
	}
}

// Create publishes the diagram.
func (r *diagramResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan diagramResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}

	published, library, err := r.publish(ctx, plan)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not publish the diagram", err)
		return
	}
	resp.Diagnostics.Append(applyPublished(&plan, published, library)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read removes the diagram from state when Plotwork no longer has it, so the
// next apply publishes it again. That happens when a plan's retention expired
// it or someone deleted it in the portal.
func (r *diagramResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state diagramResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil {
		return
	}

	var exists bool
	var err error
	switch {
	case state.Library.ValueString() != libraryPersonal && state.Library.ValueString() != "":
		exists, err = r.data.client.OrganizationDiagramExists(ctx, state.Library.ValueString(), state.ID.ValueString())
	case state.URL.ValueString() != "":
		exists, err = r.data.client.PublicDiagramExists(ctx, state.URL.ValueString())
	default:
		return // Imported with only an id: nothing to look at until the next apply publishes.
	}
	if err != nil {
		// A check that failed is not a deletion: keep the state and say so.
		resp.Diagnostics.AddWarning("Could not check the diagram in Plotwork", err.Error())
		return
	}
	if !exists {
		tflog.Info(ctx, "Diagram no longer in Plotwork; it will be published again", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
	}
}

// Update publishes the changed diagram, then deletes the one it replaced.
func (r *diagramResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state diagramResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}

	published, library, err := r.publish(ctx, plan)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not publish the changed diagram", err)
		return
	}
	resp.Diagnostics.Append(applyPublished(&plan, published, library)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	// The same content published again is the same diagram: nothing to remove.
	if (published.ID == state.ID.ValueString() && library == state.Library.ValueString()) || state.ID.IsNull() {
		return
	}
	if err := r.delete(ctx, state); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddWarning("Could not delete the diagram this one replaced",
			"The new diagram is published, but "+state.ID.ValueString()+" is still in Plotwork and counts against the plan: "+
				err.Error()+". Delete it in the portal.")
	}
}

// Delete removes the diagram. One that is already gone is not an error.
func (r *diagramResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state diagramResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.data == nil || !r.data.requireAPIKey(&resp.Diagnostics) {
		return
	}
	if err := r.delete(ctx, state); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Could not delete the diagram", err)
	}
}

// ImportState adopts a diagram by "<id>" for a personal diagram or
// "<organisation>/<id>" for an organisation's. The source is not read back,
// so the next apply publishes the configured source and replaces it.
func (r *diagramResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	library, id := libraryPersonal, req.ID
	if org, rest, found := cutLast(req.ID, "/"); found {
		library, id = org, rest
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("library"), library)...)
}

// errAliasInOrganization refuses a stable link for an organisation's
// diagram, which has no public address. The schema catches the resource's
// own organization; this catches the provider's.
var errAliasInOrganization = errors.New("alias makes a public stable link, and a diagram in an organisation's library has none: " +
	"remove alias, or publish to your own library by unsetting the provider's organization")

// publish sends the planned diagram to the right library and returns the
// answer with the library that holds it: "personal" or an organisation.
func (r *diagramResource) publish(ctx context.Context, plan diagramResourceModel) (*client.Diagram, string, error) {
	request := client.PublishRequest{
		Source:         plan.Source.ValueString(),
		Scheme:         optionalString(plan.Scheme),
		Name:           optionalString(plan.Name),
		Classification: optionalString(plan.Classification),
		Alias:          optionalString(plan.Alias),
	}
	if !plan.Folder.IsNull() {
		folder := plan.Folder.ValueString()
		request.Folder = &folder
	}
	if !plan.EmbedSource.IsNull() {
		embed := plan.EmbedSource.ValueBool()
		request.EmbedSource = &embed
	}

	if org := r.organizationOf(plan); org != "" {
		if request.Alias != "" {
			return nil, "", errAliasInOrganization
		}
		tflog.Debug(ctx, "Publishing to an organisation's library", map[string]any{"organization": org})
		published, err := r.data.client.PublishToOrganization(ctx, org, request)
		return published, org, err
	}

	published, err := r.data.client.Publish(ctx, request)
	if err != nil {
		return nil, "", err
	}
	// The organisation's Publishing setting may have sent it to its library.
	if published.Org != nil {
		return published, published.Org.ID, nil
	}
	return published, libraryPersonal, nil
}

// delete removes the diagram from the library state says holds it.
func (r *diagramResource) delete(ctx context.Context, state diagramResourceModel) error {
	if library := state.Library.ValueString(); library != libraryPersonal && library != "" {
		return r.data.client.DeleteOrganizationDiagram(ctx, library, state.ID.ValueString())
	}
	return r.data.client.DeleteDiagram(ctx, state.ID.ValueString())
}

// organizationOf is the organisation a diagram publishes to: its own
// argument, else the provider's.
func (r *diagramResource) organizationOf(plan diagramResourceModel) string {
	if org := optionalString(plan.Organization); org != "" {
		return org
	}
	return r.data.organization
}

// libraryPersonal is the library value of a diagram in a person's library.
const libraryPersonal = "personal"

// aliasPattern is the API's rule for a stable link's name.
var aliasPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// cutLast splits s around the last sep, as strings.Cut does around the first.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// applyPublished copies the API's answer and the library that holds it into
// the model.
func applyPublished(model *diagramResourceModel, d *client.Diagram, library string) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(d.ID)
	model.Bytes = types.Int64Value(d.Bytes)
	model.PublishedAt = types.StringValue(millisToRFC3339(d.PublishedAt))
	model.ExpiresAt = types.StringNull()
	if d.ExpiresAt != nil {
		model.ExpiresAt = types.StringValue(millisToRFC3339(*d.ExpiresAt))
	}

	model.URL = optionalValue(d.URL)
	model.ViewURL = optionalValue(d.ViewURL)
	model.Library = types.StringValue(library)

	model.StableURL = types.StringNull()
	if d.StableLink != nil {
		model.StableURL = types.StringValue(d.StableLink.URL)
	}

	if d.Pending {
		diags.AddWarning("The diagram is waiting for approval",
			"The organisation holds new diagrams for approval. Only its publisher and the approvers see it until an approver decides.")
	}
	return diags
}

// optionalValue is a Terraform string that is null when s is empty.
func optionalValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// millisToRFC3339 formats milliseconds since the epoch, in UTC.
func millisToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
