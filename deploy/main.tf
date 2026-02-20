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

# Tailscale auth key secret (for sidecar proxy)
resource "kubernetes_secret" "tailscale_auth" {
  metadata {
    name      = "tailscale-auth"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
  }

  data = {
    TS_AUTHKEY = var.tailscale_auth_key
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

    # Recreate strategy required: RWO PVC can't attach to two pods simultaneously
    strategy {
      type = "Recreate"
    }

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
        service_account_name = "waifu-mirror"

        # Main application container — listens on localhost only
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

        # Tailscale sidecar — disabled until fresh auth key is provisioned.
        # Uncomment and run `tofu apply` once TS_AUTHKEY is updated in the secret.
        # See: https://login.tailscale.com/admin/settings/keys
        #
        # container {
        #   name  = "tailscale"
        #   image = "ghcr.io/tailscale/tailscale:latest"
        #   env { name = "TS_AUTHKEY"
        #     value_from { secret_key_ref { name = kubernetes_secret.tailscale_auth.metadata[0].name; key = "TS_AUTHKEY" } } }
        #   env { name = "TS_HOSTNAME";      value = var.tailscale_hostname }
        #   env { name = "TS_KUBE_SECRET";   value = "" }
        #   env { name = "TS_SERVE_CONFIG";  value = "/etc/tailscale/serve.json" }
        #   env { name = "TS_STATE_DIR";     value = "/var/lib/tailscale" }
        #   volume_mount { name = "tailscale-serve-config"; mount_path = "/etc/tailscale"; read_only = true }
        #   volume_mount { name = "tailscale-state"; mount_path = "/var/lib/tailscale" }
        #   resources { requests = { cpu = "10m"; memory = "32Mi" }; limits = { cpu = "100m"; memory = "128Mi" } }
        #   security_context { capabilities { add = ["NET_ADMIN", "NET_RAW"] } }
        # }

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

# Tailscale serve config — proxy :443 (HTTPS on tailnet) to localhost:8420
resource "kubernetes_config_map" "tailscale_serve" {
  metadata {
    name      = "tailscale-serve-config"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
  }

  data = {
    "serve.json" = jsonencode({
      TCP = {
        "443" = {
          HTTPS = true
        }
      }
      Web = {
        "${var.tailscale_hostname}.taila4c78d.ts.net:443" = {
          Handlers = {
            "/" = {
              Proxy = "http://127.0.0.1:8420"
            }
          }
        }
      }
    })
  }
}

# ServiceAccount for the pod
resource "kubernetes_service_account" "waifu_mirror" {
  metadata {
    name      = "waifu-mirror"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
  }
}

# ClusterIP Service (nginx-ingress handles external access)
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

# Public Ingress — kept active until Tailscale sidecar auth is confirmed working.
# Will be removed once tailnet-only access via waifu-mirror.taila4c78d.ts.net is verified.
resource "kubernetes_ingress_v1" "waifu_mirror" {
  metadata {
    name      = "waifu-mirror"
    namespace = kubernetes_namespace.waifu_mirror.metadata[0].name
    annotations = {
      "cert-manager.io/cluster-issuer"           = "letsencrypt-prod"
      "nginx.ingress.kubernetes.io/ssl-redirect" = "true"
    }
  }

  spec {
    ingress_class_name = "nginx"

    tls {
      hosts       = [var.hostname]
      secret_name = "waifu-mirror-tls"
    }

    rule {
      host = var.hostname
      http {
        path {
          path      = "/"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service.waifu_mirror.metadata[0].name
              port {
                number = 8420
              }
            }
          }
        }
      }
    }
  }
}
