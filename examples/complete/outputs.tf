output "network_diagram_url" {
  description = "The stable link to the network diagram. It never changes; the picture behind it follows every apply."
  value       = plotwork_diagram.network.stable_url
}

output "stack_diagram_coverage" {
  description = "How much of the configuration the stack diagram shows."
  value       = data.plotwork_import.config.coverage
}
