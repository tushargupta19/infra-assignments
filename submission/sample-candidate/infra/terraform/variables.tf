variable "namespace" {
  description = "Kubernetes namespace for the config service and its database"
  type        = string
  default     = "config-service"
}

variable "kubeconfig_path" {
  description = "Path to the kubeconfig file used to reach the local cluster"
  type        = string
  default     = "~/.kube/config"
}

variable "kube_context" {
  description = "kubeconfig context pointing at the local kind/minikube cluster"
  type        = string
  default     = "kind-config-service"
}

variable "postgres_image" {
  description = "PostgreSQL image used for the local database"
  type        = string
  default     = "postgres:16-alpine"
}

variable "postgres_storage_size" {
  description = "Size of the PersistentVolumeClaim backing PostgreSQL data"
  type        = string
  default     = "1Gi"
}

variable "db_password" {
  description = <<-EOT
    PostgreSQL password. Leave empty (default) to have Terraform generate a
    random password for local use. Supply via TF_VAR_db_password to pin a
    known value, e.g. for repeatable CI runs.
  EOT
  type        = string
  sensitive   = true
  default     = ""
}

locals {
  app_name   = "config-service"
  db_name    = "configs"
  db_user    = "config_service"
  db_service = "postgres"
  db_port    = 5432
  common_labels = {
    app        = local.app_name
    managed-by = "terraform"
  }
}
