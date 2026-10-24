# A pattern file: get_pattern with file = true in the Plotwork MCP server
# gives one to start from, and check_pattern checks it.
resource "plotwork_pattern" "payments" {
  organization = "acme"
  content      = file("${path.module}/patterns/payments-service.pattern.json")
}

# Every pattern file in a folder.
resource "plotwork_pattern" "all" {
  for_each = fileset("${path.module}/patterns/approved", "*.pattern.json")

  organization = "acme"
  content      = file("${path.module}/patterns/approved/${each.value}")
}

# What agents pass to draw_from_pattern.
output "payments_pattern" {
  value = plotwork_pattern.payments.pattern_id
}
