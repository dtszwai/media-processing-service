output "image_uri" {
  value = "${var.ecr_repository_url}:${local.image_tag}"
}
