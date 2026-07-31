resource "kubernetes_namespace" "config_service" {
  metadata {
    name   = var.namespace
    labels = local.common_labels
  }
}
