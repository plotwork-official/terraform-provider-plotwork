# A complete example: two diagrams of one stack, kept up to date by every apply.
#
# 1. The configuration itself, drawn by plotwork_import and stored in S3.
# 2. A hand-written diagram filled with live values (CIDRs, DNS names,
#    endpoints) from module outputs, published with a stable link.
#
# Run it with PLOTWORK_API_KEY set. plotwork_import and plotwork_render need
# the Pro plan; plotwork_diagram publishes on every plan.

terraform {
  required_version = ">= 1.5"
  required_providers {
    plotwork = {
      source  = "plotwork-official/plotwork"
      version = "~> 0.1"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "plotwork" {}

provider "aws" {
  region = var.region
}

# 1. Draw this folder's Terraform and keep the SVG in the docs bucket.
data "plotwork_import" "config" {
  format = "terraform"
  title  = "${var.environment} stack"
  files = {
    for f in fileset(path.module, "*.tf") : f => file("${path.module}/${f}")
  }
}

data "plotwork_render" "config" {
  source = data.plotwork_import.config.source
  scheme = "auto"
}

resource "aws_s3_object" "config_diagram" {
  bucket       = var.docs_bucket
  key          = "architecture/${var.environment}/stack.svg"
  content      = data.plotwork_render.config.svg
  content_type = "image/svg+xml"
}

# 2. Fill a diagram with what the apply produced, and publish it. Because the
#    template reads resource attributes, Terraform publishes it after those
#    resources exist and again whenever one of the values changes.
resource "plotwork_diagram" "network" {
  name   = "${var.environment} network"
  folder = "Platform/${var.environment}"
  scheme = "auto"
  alias  = "${var.environment}-network"

  source = templatefile("${path.module}/network.diagram.tftpl", {
    environment = var.environment
    vpc_cidr    = aws_vpc.main.cidr_block
    subnets     = { for k, s in aws_subnet.app : "app-${k}" => s.cidr_block }
    lb_dns_name = aws_lb.app.dns_name
    db_endpoint = aws_db_instance.main.address
  })
}
