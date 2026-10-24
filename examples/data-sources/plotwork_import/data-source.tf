# Draw the Terraform files of this module.
data "plotwork_import" "network" {
  format = "terraform"
  title  = "Production network"
  files = {
    for f in fileset(path.module, "*.tf") : f => file("${path.module}/${f}")
  }
}

# Draw a Kubernetes manifest.
data "plotwork_import" "cluster" {
  format = "kubernetes"
  text   = file("${path.module}/k8s/app.yaml")
}

output "network_coverage" {
  value = data.plotwork_import.network.coverage
}
