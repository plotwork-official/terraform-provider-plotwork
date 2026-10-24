# Terraform provider for Plotwork

Draw your infrastructure as code as [Plotwork](https://plotwork.dev) diagrams, and keep them current: the diagram is part of the configuration, so every apply that changes the infrastructure changes the picture. No extra CI job, no agent.

Works with Terraform 1.9+ and OpenTofu.

| Block | Does | Plan |
|---|---|---|
| [`plotwork_import`](docs/data-sources/import.md) | Turns Terraform, plan JSON, Bicep, Kubernetes, Compose, SQL and more into diagram source | Pro |
| [`plotwork_render`](docs/data-sources/render.md) | Renders source to an SVG you write anywhere: S3, Azure Storage, GCS, a file | Pro |
| [`plotwork_diagram`](docs/resources/diagram.md) | Publishes to your library or your organisation's, with a stable link | Every plan |

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

[`examples/complete`](examples/complete) does all three for one stack.

## Authentication

| Variable | Provider argument | What |
|---|---|---|
| `PLOTWORK_API_KEY` | `api_key` | A personal key, or an organisation key from **Keys and CI** |
| `PLOTWORK_ORG` | `organization` | The organisation `plotwork_diagram` publishes to. Needed with an organisation key |
| `PLOTWORK_API_URL` | `api_url` | Another API, such as a preview environment |

## What leaves your machine

`plotwork_import` sends the files you give it; the others send diagram source. Plotwork works on it in memory and keeps nothing unless `plotwork_diagram` publishes it. Pass configuration, not state: state can hold secrets.

## Documentation

* [Provider docs](docs/index.md), also on the Terraform Registry
* [Plotwork diagram language](https://docs.plotwork.dev/writing-diagrams)
* [Importing from code and data](https://docs.plotwork.dev/importing-from-code-and-data)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md).

## Licence

[MIT](LICENSE)
