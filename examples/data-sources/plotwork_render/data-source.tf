data "plotwork_import" "network" {
  format = "terraform"
  files  = { for f in fileset(path.module, "*.tf") : f => file("${path.module}/${f}") }
}

data "plotwork_render" "network" {
  source = data.plotwork_import.network.source
  scheme = "auto"
}

# Keep the picture next to the architecture docs.
resource "aws_s3_object" "network" {
  bucket       = "acme-architecture"
  key          = "diagrams/network.svg"
  content      = data.plotwork_render.network.svg
  content_type = "image/svg+xml"
}
