########################################
# Credentials
########################################

# Generates a random local-only password when the caller does not supply
# TF_VAR_db_password. Marked sensitive so it never appears in CLI output.
resource "random_password" "db_password" {
  count   = var.db_password == "" ? 1 : 0
  length  = 24
  special = false
}

locals {
  db_password = var.db_password != "" ? var.db_password : random_password.db_password[0].result

  # Cluster-internal DSN. sslmode=disable is acceptable for a local, single-node
  # Postgres reached over the in-cluster ClusterIP network; a production
  # deployment would terminate TLS and use sslmode=require (see README).
  database_url = "postgres://${local.db_user}:${local.db_password}@${local.db_service}.${var.namespace}.svc.cluster.local:${local.db_port}/${local.db_name}?sslmode=disable"
}

# Credentials consumed by the PostgreSQL container itself.
resource "kubernetes_secret" "postgres" {
  metadata {
    name      = "postgres-credentials"
    namespace = kubernetes_namespace.config_service.metadata[0].name
    labels    = local.common_labels
  }

  data = {
    POSTGRES_USER     = local.db_user
    POSTGRES_PASSWORD = local.db_password
    POSTGRES_DB       = local.db_name
  }

  type = "Opaque"
}

# Connection string consumed by config-service (referenced by k8s/deployment.yaml
# as secretKeyRef config-service-db/DATABASE_URL). Terraform owns this secret
# so the app deployment never needs the raw Postgres credentials directly.
resource "kubernetes_secret" "config_service_db" {
  metadata {
    name      = "config-service-db"
    namespace = kubernetes_namespace.config_service.metadata[0].name
    labels    = local.common_labels
  }

  data = {
    DATABASE_URL = local.database_url
  }

  type = "Opaque"
}

########################################
# PostgreSQL
########################################

resource "kubernetes_stateful_set" "postgres" {
  metadata {
    name      = "postgres"
    namespace = kubernetes_namespace.config_service.metadata[0].name
    labels    = local.common_labels
  }

  spec {
    service_name = local.db_service
    replicas     = 1

    selector {
      match_labels = {
        app = local.db_service
      }
    }

    template {
      metadata {
        labels = {
          app = local.db_service
        }
      }

      spec {
        container {
          name  = "postgres"
          image = var.postgres_image

          port {
            name           = "postgres"
            container_port = local.db_port
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.postgres.metadata[0].name
            }
          }

          # Persists data under a dedicated subdirectory so PGDATA is never
          # written directly to the PVC mount root (avoids postgres's
          # "directory not empty" complaint about lost+found on some CSI drivers).
          env {
            name  = "PGDATA"
            value = "/var/lib/postgresql/data/pgdata"
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/lib/postgresql/data"
          }

          readiness_probe {
            exec {
              command = ["pg_isready", "-U", local.db_user, "-d", local.db_name]
            }
            initial_delay_seconds = 5
            period_seconds        = 5
            failure_threshold     = 6
          }

          liveness_probe {
            exec {
              command = ["pg_isready", "-U", local.db_user, "-d", local.db_name]
            }
            initial_delay_seconds = 15
            period_seconds        = 10
            failure_threshold     = 6
          }

          resources {
            requests = {
              cpu    = "100m"
              memory = "128Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
          }
        }
      }
    }

    volume_claim_template {
      metadata {
        name = "data"
      }
      spec {
        access_modes = ["ReadWriteOnce"]
        resources {
          requests = {
            storage = var.postgres_storage_size
          }
        }
      }
    }
  }
}

# ClusterIP (non-headless) service is sufficient here: the app connects to a
# single Postgres replica by service DNS name, and a headless service brings
# no benefit without multiple StatefulSet replicas.
resource "kubernetes_service" "postgres" {
  metadata {
    name      = local.db_service
    namespace = kubernetes_namespace.config_service.metadata[0].name
    labels    = local.common_labels
  }

  spec {
    selector = {
      app = local.db_service
    }

    port {
      name        = "postgres"
      port        = local.db_port
      target_port = local.db_port
    }

    type = "ClusterIP"
  }
}
