# Terraform provider for Plotwork

Draw your infrastructure as code as [Plotwork](https://plotwork.dev) diagrams, and keep them current: the diagram is part of the configuration, so every apply that changes the infrastructure changes the picture. No extra CI job, no agent.

Works with Terraform 1.9+ and OpenTofu.

| Block | Does | Plan |
|---|---|---|
| [`plotwork_import`](docs/data-sources/import.md) | Turns Terraform, plan JSON, Bicep, Kubernetes, Compose, SQL and more into diagram source | Pro |
| [`plotwork_render`](docs/data-sources/render.md) | Renders source to an SVG you write anywhere: S3, Azure Storage, GCS, a file | Pro |
| [`plotwork_diagram`](docs/resources/diagram.md) | Publishes to your library or your organisation's, with a stable link | Every plan |
| [`plotwork_organization_brand`](docs/resources/organization_brand.md) | Sets the colours an organisation's diagrams are painted in | Pro |
| [`plotwork_organization_icon`](docs/resources/organization_icon.md) | Uploads a custom icon (`icon=org:<name>`) from a file, an S3 or Azure blob, or a CDN URL | Pro |
| [`plotwork_pattern`](docs/resources/pattern.md) | Saves an approved pattern from a `.pattern.json` file, for agents to start from | Enterprise |

## Quick start

```hcl
terraform {
  required_providers {
    plotwork = {
      source  = "plotwork-official/plotwork"
      version = "~> 0.1"
    }
  }
}

provider "plotwork" {} # reads PLOTWORK_API_KEY

# Draw this module's Terraform.
data "plotwork_import" "network" {
  format = "terraform"
  files  = { for f in fileset(path.module, "*.tf") : f => file("${path.module}/${f}") }
}

# Publish it with a link that never changes.
resource "plotwork_diagram" "network" {
  source = data.plotwork_import.network.source
  name   = "Production network"
  alias  = "prod-network"
}

output "diagram" {
  value = plotwork_diagram.network.stable_url
}
```

```shell
export PLOTWORK_API_KEY="pw_..."   # from https://app.plotwork.dev, API keys
terraform apply
```

## Three ways to use it

| You want | Use |
|---|---|
| A picture of the code, in your own bucket | `plotwork_import` → `plotwork_render` → `aws_s3_object`, `azurerm_storage_blob` or `local_file` |
| A picture with live values (CIDRs, endpoints, DNS names) | `templatefile()` filled with resource attributes → `plotwork_diagram` |
| Diagrams the team browses in Plotwork | `plotwork_diagram` with `organization`, or an organisation key |
| The organisation's brand, icons and approved patterns in code | `plotwork_organization_brand`, `plotwork_organization_icon`, `plotwork_pattern`, with a key that has the `design:write` scope |

[`examples/complete`](examples/complete) does all three for one stack, and [terraform-plotwork-examples](https://github.com/plotwork-official/terraform-plotwork-examples) has ten runnable examples, among them an organisation's brand, icons and patterns, with the smoke test each release passes.

## Authentication

| Variable | Provider argument | What |
|---|---|---|
| `PLOTWORK_API_KEY` | `api_key` | A personal key, or an organisation key from **Keys and CI** |
| `PLOTWORK_ORG` | `organization` | The organisation `plotwork_diagram` publishes to, and whose brand, icons and patterns the design resources manage. Needed with an organisation key |
| `PLOTWORK_API_URL` | `api_url` | Another API, such as a preview environment |

## Release channels

| Version | Talks to by default | Get it with |
|---|---|---|
| Stable, such as `0.2.0` | Production, `https://api.plotwork.dev` | `version = "~> 0.2"` |
| Pre-release, such as `0.3.0-beta.1` | Preview, `https://api-preview.plotwork.dev` | `version = "0.3.0-beta.1"`: a range never picks a pre-release |

`api_url` or `PLOTWORK_API_URL` overrides either. How releases are made: [RELEASING.md](RELEASING.md).

## What leaves your machine

`plotwork_import` sends the files you give it; the others send diagram source, or the colours, icon files and pattern files you configure. A `source_url` icon is downloaded by the provider, on your machine, during every plan. Plotwork works on it in memory and keeps nothing unless `plotwork_diagram` publishes it. Pass configuration, not state: state can hold secrets.

## Documentation

* [Provider docs](docs/index.md), also on the Terraform Registry
* [Plotwork diagram language](https://docs.plotwork.dev/writing-diagrams)
* [Importing from code and data](https://docs.plotwork.dev/importing-from-code-and-data)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md).

## Licence

[MIT](LICENSE)
