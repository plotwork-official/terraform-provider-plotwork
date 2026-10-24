data "plotwork_import" "network" {
  format = "terraform"
  files  = { for f in fileset(path.module, "*.tf") : f => file("${path.module}/${f}") }
}

# A personal diagram with a stable link that never changes.
resource "plotwork_diagram" "network" {
  source = data.plotwork_import.network.source
  name   = "Production network"
  folder = "Platform/Network"
  scheme = "auto"
  alias  = "prod-network"
}

# A diagram in an organisation's private library.
resource "plotwork_diagram" "network_team" {
  source         = data.plotwork_import.network.source
  name           = "Production network"
  organization   = "acme"
  classification = "internal"
}

output "network_diagram" {
  value = plotwork_diagram.network.stable_url
}
