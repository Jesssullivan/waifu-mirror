output "namespace" {
  value = kubernetes_namespace.waifu_mirror.metadata[0].name
}

output "service_name" {
  value = kubernetes_service.waifu_mirror.metadata[0].name
}

output "cluster_endpoint" {
  value     = data.civo_kubernetes_cluster.cluster.api_endpoint
  sensitive = true
}
