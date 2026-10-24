# The organisation's brand. secondary and accent are made from primary
# when left out; effective_secondary and effective_accent say which.
resource "plotwork_organization_brand" "acme" {
  organization = "acme"
  primary      = "#2a78d6"
  secondary    = "#7c3aed"
}

output "brand_accent" {
  value = plotwork_organization_brand.acme.effective_accent
}
