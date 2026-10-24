// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

// schemes are the colour schemes a diagram is painted in. auto is one SVG that
// follows the viewer's light or dark setting.
var schemes = []string{"light", "dark", "auto"}

var (
	_ datasource.DataSource              = (*renderDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*renderDataSource)(nil)
)

// renderDataSource is plotwork_render: Plotwork source in, SVG out.
type renderDataSource struct {
	data *providerData
}

// renderDataSourceModel is the plotwork_render block and its results.
type renderDataSourceModel struct {
	Source      types.String `tfsdk:"source"`
	Scheme      types.String `tfsdk:"scheme"`
	Theme       types.String `tfsdk:"theme"`
	EmbedSource types.Bool   `tfsdk:"embed_source"`

	ID       types.String `tfsdk:"id"`
	SVG      types.String `tfsdk:"svg"`
	Width    types.Int64  `tfsdk:"width"`
	Height   types.Int64  `tfsdk:"height"`
	Warnings types.List   `tfsdk:"warnings"`
}

func newRenderDataSource() datasource.DataSource {
	return &renderDataSource{}
}

// Metadata names the data source plotwork_render.
func (d *renderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_render"
}

// Schema describes plotwork_render.
func (d *renderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Renders Plotwork source to a self-contained SVG. Write `svg` wherever the team reads it: " +
			"an `aws_s3_object`, an `azurerm_storage_blob`, a `google_storage_bucket_object` or a `local_file`. " +
			"Nothing is stored by Plotwork. Needs the Pro plan or above.",
		Attributes: map[string]schema.Attribute{
			"source": schema.StringAttribute{
				MarkdownDescription: "The Plotwork source to draw: from `plotwork_import`, a `.diagram` file, or `templatefile()` " +
					"filled with module outputs.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"scheme": schema.StringAttribute{
				MarkdownDescription: "`light` (the default), `dark`, or `auto` for one picture that follows the viewer's setting.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf(schemes...)},
			},
			"theme": schema.StringAttribute{
				MarkdownDescription: "A colour theme to paint with, over the source's own `theme=`.",
				Optional:            true,
			},
			"embed_source": schema.BoolAttribute{
				MarkdownDescription: "Write the source into the SVG's metadata so Plotwork can open it for editing later. " +
					"Anyone with the file can then read the source. Defaults to false.",
				Optional: true,
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "The SHA-256 of `svg`.",
				Computed:            true,
			},
			"svg": schema.StringAttribute{
				MarkdownDescription: "The SVG markup, with an accessible title and description.",
				Computed:            true,
			},
			"width": schema.Int64Attribute{
				MarkdownDescription: "The picture's width in pixels.",
				Computed:            true,
			},
			"height": schema.Int64Attribute{
				MarkdownDescription: "The picture's height in pixels.",
				Computed:            true,
			},
			"warnings": schema.ListAttribute{
				MarkdownDescription: "Quality warnings, one line each with its code, place and hint. They never stop the render.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

// Configure keeps the provider's client.
func (d *renderDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// Read renders the source.
func (d *renderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model renderDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || d.data == nil || !d.data.requireAPIKey(&resp.Diagnostics) {
		return
	}

	result, err := d.data.client.Render(ctx, client.RenderRequest{
		Source:      model.Source.ValueString(),
		Scheme:      optionalString(model.Scheme),
		Theme:       optionalString(model.Theme),
		EmbedSource: model.EmbedSource.ValueBool(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Plotwork render failed", err)
		return
	}

	model.ID = types.StringValue(contentID(result.SVG))
	model.SVG = types.StringValue(result.SVG)
	model.Width = types.Int64Value(int64(result.Size.Width))
	model.Height = types.Int64Value(int64(result.Size.Height))
	model.Warnings = stringList(formatDiagnostics(result.Warnings))

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
