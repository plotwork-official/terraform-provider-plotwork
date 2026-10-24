// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

// Package provider implements the Plotwork Terraform provider: data sources
// that turn infrastructure code into Plotwork diagrams and SVG, and a resource
// that publishes a diagram to a person's or an organisation's library.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/plotwork-official/terraform-provider-plotwork/internal/client"
)

// Environment variables the provider reads when its block leaves a value out.
// They are the names the Plotwork CLI and GitHub Action read.
const (
	envAPIKey       = "PLOTWORK_API_KEY"
	envAPIURL       = "PLOTWORK_API_URL"
	envOrganization = "PLOTWORK_ORG"
)

var _ provider.Provider = (*plotworkProvider)(nil)

// plotworkProvider is the provider's configuration surface.
type plotworkProvider struct {
	// version is the release version, "dev" for a local build and "test"
	// in tests. It is sent in the User-Agent.
	version string
	// defaultAPIURL is the API used when neither api_url nor PLOTWORK_API_URL
	// names one. A release sets it by channel: production for a stable
	// version, preview for a pre-release such as 0.2.0-beta.1.
	defaultAPIURL string
}

// plotworkProviderModel is the provider block.
type plotworkProviderModel struct {
	APIKey       types.String `tfsdk:"api_key"`
	APIURL       types.String `tfsdk:"api_url"`
	Organization types.String `tfsdk:"organization"`
}

// providerData is what Configure hands to every data source and resource.
type providerData struct {
	client *client.Client
	// hasAPIKey is false when neither the block nor the environment gave a
	// key, so a call can say how to set one before the API refuses it.
	hasAPIKey bool
	// organization is the default organisation slug or id for
	// plotwork_diagram, or empty for the caller's own library.
	organization string
}

// New returns the constructor the plugin server calls for each session.
// defaultAPIURL is the API to use when the configuration names none; empty
// means the production API.
func New(version, defaultAPIURL string) func() provider.Provider {
	if defaultAPIURL == "" {
		defaultAPIURL = client.DefaultBaseURL
	}
	return func() provider.Provider {
		return &plotworkProvider{version: version, defaultAPIURL: defaultAPIURL}
	}
}

// Metadata names the provider: every type it serves starts with "plotwork_".
func (p *plotworkProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "plotwork"
	resp.Version = p.version
}

// Schema describes the provider block.
func (p *plotworkProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Draw infrastructure as code as [Plotwork](https://plotwork.dev) diagrams, render them to SVG, " +
			"and publish them to your own or your organisation's library, so the diagram changes on every apply.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "A Plotwork API key: a personal key, or an organisation key from **Keys and CI**. " +
					"Defaults to the `" + envAPIKey + "` environment variable. Prefer the variable to a value in code.",
				Optional:  true,
				Sensitive: true,
			},
			"api_url": schema.StringAttribute{
				MarkdownDescription: "The API's address. Defaults to `" + envAPIURL + "`, else the API of the provider's release channel: " +
					"`" + client.DefaultBaseURL + "` for a stable version, `" + client.PreviewBaseURL + "` for a pre-release such as `0.2.0-beta.1`.",
				Optional: true,
			},
			"organization": schema.StringAttribute{
				MarkdownDescription: "The slug or id of the organisation `plotwork_diagram` publishes to when it names none. " +
					"Defaults to `" + envOrganization + "`. Required for an organisation key, which publishes only to its own organisation.",
				Optional: true,
			},
		},
	}
}

// Configure builds the API client. A missing key is not an error here: a
// configuration that only formats text with Terraform functions needs none,
// and each call that needs a key says so when it runs.
func (p *plotworkProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config plotworkProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A value that is unknown during plan (computed from another resource)
	// cannot configure the client yet.
	for name, value := range map[string]types.String{"api_key": config.APIKey, "api_url": config.APIURL, "organization": config.Organization} {
		if value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider value",
				"The provider cannot be configured with a value that is only known after apply. Set "+name+" from a variable, or use its environment variable.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := valueOrEnv(config.APIKey, envAPIKey)
	apiURL := valueOrEnv(config.APIURL, envAPIURL)
	if apiURL == "" {
		apiURL = p.defaultAPIURL
	}
	data := &providerData{
		client: client.New(apiURL, apiKey,
			client.WithUserAgent("terraform-provider-plotwork/"+p.version+" (+https://registry.terraform.io/providers/plotwork-official/plotwork)"),
		),
		hasAPIKey:    apiKey != "",
		organization: valueOrEnv(config.Organization, envOrganization),
	}
	ctx = tflog.SetField(ctx, "plotwork_api_url", apiURL)
	tflog.Debug(ctx, "Configured Plotwork client", map[string]any{"has_api_key": apiKey != "", "organization": data.organization})

	resp.DataSourceData = data
	resp.ResourceData = data
}

// Resources lists the resource types the provider serves.
func (p *plotworkProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newDiagramResource,
	}
}

// DataSources lists the data source types the provider serves.
func (p *plotworkProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newImportDataSource,
		newRenderDataSource,
	}
}

// valueOrEnv returns the configured value, else the environment variable.
func valueOrEnv(value types.String, env string) string {
	if !value.IsNull() && !value.IsUnknown() {
		return value.ValueString()
	}
	return os.Getenv(env)
}
