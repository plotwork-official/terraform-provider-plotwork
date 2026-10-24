# From a file in this module.
resource "plotwork_organization_icon" "payments" {
  organization = "acme"
  name         = "payments-api"
  svg          = file("${path.module}/icons/payments-api.svg")
  aliases      = ["payments service"]
}

# Every SVG in a folder, one icon each, named after the file.
resource "plotwork_organization_icon" "team" {
  for_each = fileset("${path.module}/icons/team", "*.svg")

  organization = "acme"
  name         = trimsuffix(each.value, ".svg")
  svg          = file("${path.module}/icons/team/${each.value}")
}

# From an S3 object. The aws provider fills body only for text types such
# as application/xml and text/*, not image/svg+xml: store the SVG as
# application/xml, or give source_url a pre-signed address instead.
data "aws_s3_object" "ledger" {
  bucket = "acme-design"
  key    = "icons/ledger.svg"
}

resource "plotwork_organization_icon" "ledger" {
  organization = "acme"
  name         = "ledger"
  svg          = data.aws_s3_object.ledger.body
}

# From Azure Blob Storage. azurerm_storage_blob does not read a blob's
# contents, so download it with a read-only SAS address.
data "http" "queue" {
  url = "https://acmedesign.blob.core.windows.net/icons/queue.svg?${var.icons_sas}"
}

resource "plotwork_organization_icon" "queue" {
  organization = "acme"
  name         = "queue"
  svg          = data.http.queue.response_body
}

# From a CDN: the provider downloads the address during every plan, so a
# new file there is a planned change.
resource "plotwork_organization_icon" "gateway" {
  organization = "acme"
  name         = "gateway"
  source_url   = "https://cdn.acme.example/icons/gateway.svg"
}

# A diagram draws it with icon=org:payments-api.
output "payments_icon" {
  value = plotwork_organization_icon.payments.ref
}

variable "icons_sas" {
  type      = string
  sensitive = true
}
