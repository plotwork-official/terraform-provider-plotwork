// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

var (
	_ datasource.DataSource              = (*importDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*importDataSource)(nil)
)

// importDataSource is plotwork_import: files of another format in, Plotwork
// source out.
type importDataSource struct {
	data *providerData
}

// importDataSourceModel is the plotwork_import block and its results.
type importDataSourceModel struct {
	Format   types.String `tfsdk:"format"`
	Text     types.String `tfsdk:"text"`
	Files    types.Map    `tfsdk:"files"`
	Title    types.String `tfsdk:"title"`
	View     types.String `tfsdk:"view"`
	GroupBy  types.String `tfsdk:"group_by"`
	Root     types.String `tfsdk:"root"`
	VarFiles types.List   `tfsdk:"var_files"`
	Icons    types.String `tfsdk:"icons"`

	ID         types.String `tfsdk:"id"`
	Source     types.String `tfsdk:"source"`
	Kind       types.String `tfsdk:"kind"`
	Summary    types.String `tfsdk:"summary"`
	Coverage   types.String `tfsdk:"coverage"`
	Complete   types.Bool   `tfsdk:"complete"`
	PartsTotal types.Int64  `tfsdk:"parts_total"`
	Warnings   types.List   `tfsdk:"warnings"`
	Outline    types.List   `tfsdk:"outline"`
}

func newImportDataSource() datasource.DataSource {
	return &importDataSource{}
}

// Metadata names the data source plotwork_import.
func (d *importDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_import"
}

// Schema describes plotwork_import.
func (d *importDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Turns files of another format into Plotwork diagram source: Terraform, Terraform plan or state JSON, " +
			"Bicep, ARM, CloudFormation, Pulumi, Kubernetes manifests, Helm, Docker Compose, SQL, OpenAPI and more. " +
			"Pass `source` to `plotwork_render` for an SVG or to `plotwork_diagram` to publish it.\n\n" +
			"The files are sent to the Plotwork API, worked on in memory, and not stored. Send configuration, not state: " +
			"state can hold secrets. Needs the Pro plan or above.",
		Attributes: map[string]schema.Attribute{
			"format": schema.StringAttribute{
				MarkdownDescription: "What the files are, such as `terraform`, `terraform-plan`, `kubernetes`, `compose`, `bicep` or `sql`. " +
					"The [importing from code and data](https://docs.plotwork.dev/importing-from-code-and-data) guide lists every value.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"text": schema.StringAttribute{
				MarkdownDescription: "The content of one file. Set `text` or `files`, not both.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("files")),
				},
			},
			"files": schema.MapAttribute{
				MarkdownDescription: "The files of a folder, by path, for a format that spans files such as a Terraform module: " +
					"`{ for f in fileset(path.module, \"*.tf\") : f => file(\"${path.module}/${f}\") }`.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The diagram's title. Taken from the files when left out.",
				Optional:            true,
			},
			"view": schema.StringAttribute{
				MarkdownDescription: "For a format drawn several ways, which picture to draw.",
				Optional:            true,
			},
			"group_by": schema.StringAttribute{
				MarkdownDescription: "How resources are framed, such as `module` (the default for Terraform).",
				Optional:            true,
			},
			"root": schema.StringAttribute{
				MarkdownDescription: "The root module, stack or template to draw when the files hold several.",
				Optional:            true,
			},
			"var_files": schema.ListAttribute{
				MarkdownDescription: "Paths of variables files among `files` to apply, such as `prod.tfvars`.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"icons": schema.StringAttribute{
				MarkdownDescription: "`auto` (the default) puts a provider's icon on each resource; `off` draws none.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("auto", "off")},
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "The SHA-256 of `source`.",
				Computed:            true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "The Plotwork source of the diagram.",
				Computed:            true,
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "The kind of diagram the files became, such as `flow` or `schema`.",
				Computed:            true,
			},
			"summary": schema.StringAttribute{
				MarkdownDescription: "A one-line description of the diagram, usable as alt text.",
				Computed:            true,
			},
			"coverage": schema.StringAttribute{
				MarkdownDescription: "How much of the files the diagram shows, in one sentence.",
				Computed:            true,
			},
			"complete": schema.BoolAttribute{
				MarkdownDescription: "True when nothing in the files was left out of the diagram.",
				Computed:            true,
			},
			"parts_total": schema.Int64Attribute{
				MarkdownDescription: "When the files are too big for one readable diagram, how many diagrams they were split into; " +
					"`source` is the first. 0 when the import is one diagram.",
				Computed: true,
			},
			"warnings": schema.ListAttribute{
				MarkdownDescription: "What was left out or changed, one line each with its code, place and hint.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"outline": schema.ListAttribute{
				MarkdownDescription: "The diagram as plain text, to check it says what the files say.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

// Configure keeps the provider's client.
func (d *importDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// Read sends the files to the API and stores the source it made.
func (d *importDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model importDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || d.data == nil || !d.data.requireAPIKey(&resp.Diagnostics) {
		return
	}

	request := client.ImportRequest{
		Format: model.Format.ValueString(),
		Text:   optionalString(model.Text),
		Title:  optionalString(model.Title),
		Options: client.ImportOptions{
			View:    optionalString(model.View),
			GroupBy: optionalString(model.GroupBy),
			Root:    optionalString(model.Root),
			Icons:   optionalString(model.Icons),
		},
	}
	if !model.Files.IsNull() {
		var files map[string]string
		resp.Diagnostics.Append(model.Files.ElementsAs(ctx, &files, false)...)
		request.Files = sortedFiles(files)
	}
	if !model.VarFiles.IsNull() {
		resp.Diagnostics.Append(model.VarFiles.ElementsAs(ctx, &request.Options.VarFiles, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.data.client.Import(ctx, request)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Plotwork import failed", err)
		return
	}
	if !result.OK {
		resp.Diagnostics.AddError("Plotwork could not import the files",
			"Nothing was imported:\n"+strings.Join(formatDiagnostics(result.Diagnostics), "\n"))
		return
	}

	model.ID = types.StringValue(contentID(result.Source))
	model.Source = types.StringValue(result.Source)
	model.Kind = nullableString(result.Kind)
	model.Summary = types.StringNull()
	model.Coverage = types.StringNull()
	model.Complete = types.BoolValue(true)
	model.PartsTotal = types.Int64Value(int64(result.PartsTotal))
	model.Outline = stringList(result.Outline)

	warnings := formatDiagnostics(result.Diagnostics)
	if result.Validation != nil {
		model.Summary = nullableString(result.Validation.Summary)
		warnings = append(warnings, formatDiagnostics(result.Validation.Warnings)...)
	}
	if result.Coverage != nil {
		model.Coverage = types.StringValue(result.Coverage.Line)
		model.Complete = types.BoolValue(result.Coverage.Complete)
	}
	model.Warnings = stringList(warnings)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// sortedFiles turns the files map into the API's list, ordered by path so
// that the same files always make the same request.
func sortedFiles(files map[string]string) []client.ImportFile {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	out := make([]client.ImportFile, 0, len(paths))
	for _, p := range paths {
		out = append(out, client.ImportFile{Path: p, Text: files[p]})
	}
	return out
}
