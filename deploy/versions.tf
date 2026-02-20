terraform {
  required_version = ">= 1.6.0"

  required_providers {
    civo = {
      source  = "civo/civo"
      version = "~> 1.1"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.35"
    }
  }
}
