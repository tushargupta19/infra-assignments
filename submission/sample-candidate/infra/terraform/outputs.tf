output "namespace" {
  description = "Namespace the config service and database are deployed into"
  value       = kubernetes_namespace.config_service.metadata[0].name
}

output "postgres_service" {
  description = "In-cluster DNS name of the PostgreSQL service"
  value       = "${local.db_service}.${var.namespace}.svc.cluster.local"
}

output "database_secret_name" {
  description = "Name of the Secret containing DATABASE_URL for config-service"
  value       = kubernetes_secret.config_service_db.metadata[0].name
}
