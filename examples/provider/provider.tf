terraform {
  required_providers {
    plotwork = {
      source  = "plotwork-official/plotwork"
      version = "~> 0.1"
    }
  }
}

# Reads PLOTWORK_API_KEY, and PLOTWORK_ORG when set.
provider "plotwork" {}
