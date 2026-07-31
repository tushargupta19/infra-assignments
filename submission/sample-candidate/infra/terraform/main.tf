terraform {
  required_version = ">= 1.8"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.31"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

# Points at the local kind/minikube cluster via the standard kubeconfig file.
# Using config_path + config_context (rather than in-cluster config) is the
# right choice here: Terraform runs from the operator's machine against a
# local cluster, not from inside the cluster itself.
provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kube_context
}
