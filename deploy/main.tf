provider "civo" {
  # Token read from CIVO_TOKEN env var (set by CI or TF_VAR_civo_token).
  region = var.civo_region
}

# Reference existing CIVO K3s cluster.
data "civo_kubernetes_cluster" "cluster" {
  name = var.cluster_name
}

locals {
  kube = yamldecode(data.civo_kubernetes_cluster.cluster.kubeconfig)
}

provider "kubernetes" {
  host                   = local.kube.clusters[0].cluster.server
  cluster_ca_certificate = base64decode(local.kube.clusters[0].cluster["certificate-authority-data"])
  client_certificate     = try(base64decode(local.kube.users[0].user["client-certificate-data"]), null)
  client_key             = try(base64decode(local.kube.users[0].user["client-key-data"]), null)
  token                  = try(local.kube.users[0].user.token, null)
}

# Namespace
resource "kubernetes_namespace" "waifu_mirror" {
  metadata {
    name = var.namespace
    labels = {
      app        = "waifu-mirror"
      managed-by = "opentofu"
    }
  }
}

# PVC for image catalog persistence
resource "kubernetes_persistent_volume_claim" "data" {
  metadata {
    name      = "waifu-mirror-data"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
  }

  wait_until_bound = false

  spec {
    access_modes = ["ReadWriteOnce"]
    resources {
      requests = {
        storage = var.storage_size
      }
    }
  }
}

# Deployment
resource "kubernetes_deployment" "waifu_mirror" {
  metadata {
    name      = "waifu-mirror"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
    labels = {
      app        = "waifu-mirror"
      managed-by = "opentofu"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "waifu-mirror"
      }
    }

    template {
      metadata {
        labels = {
          app = "waifu-mirror"
        }
      }

      spec {
        container {
          name  = "waifu-mirror"
          image = "ghcr.io/jesssullivan/waifu-mirror:${var.image_tag}"

          args = [
            "-data", "/data",
            "-tailnet-only=false",
            "-cron", var.ingest_interval,
          ]

          port {
            container_port = 8420
            name           = "http"
          }

          volume_mount {
            name       = "data"
            mount_path = "/data"
          }

          resources {
            requests = {
              cpu    = "50m"
              memory = "128Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
          }

          liveness_probe {
            http_get {
              path = "/api/health"
              port = "http"
            }
            initial_delay_seconds = 5
            period_seconds        = 30
          }

          readiness_probe {
            http_get {
              path = "/api/health"
              port = "http"
            }
            initial_delay_seconds = 2
            period_seconds        = 10
          }
        }

        volume {
          name = "data"
          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim.data.metadata[0].name
          }
        }
      }
    }
  }
}

# ClusterIP Service (internal only)
resource "kubernetes_service" "waifu_mirror" {
  metadata {
    name      = "waifu-mirror"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
    labels = {
      app        = "waifu-mirror"
      managed-by = "opentofu"
    }
  }

  spec {
    selector = {
      app = "waifu-mirror"
    }

    port {
      port        = 8420
      target_port = "http"
      name        = "http"
    }

    type = "ClusterIP"
  }
}
