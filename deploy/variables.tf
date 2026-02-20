variable "civo_region" {
  description = "CIVO region"
  type        = string
  default     = "NYC1"
}

variable "cluster_name" {
  description = "Existing CIVO K8s cluster name"
  type        = string
  default     = "tinyland-civo-dev"
}

variable "namespace" {
  description = "Kubernetes namespace for waifu-mirror"
  type        = string
  default     = "waifu-mirror"
}

variable "image_tag" {
  description = "Container image tag"
  type        = string
  default     = "latest"
}

variable "storage_size" {
  description = "PVC storage size"
  type        = string
  default     = "2Gi"
}

variable "ingest_interval" {
  description = "Background ingest cron interval"
  type        = string
  default     = "1h"
}

variable "hostname" {
  description = "Public hostname for ingress TLS"
  type        = string
  default     = "waifu.ephemera.tinyland.dev"
}
