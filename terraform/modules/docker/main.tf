locals {
  files_to_hash = setsubtract(
    fileset(var.docker_build_context, "**/*"),
    fileset(var.docker_build_context, "node_modules/**/*")
  )
  file_hashes = {
    for file in local.files_to_hash :
    file => filesha256("${var.docker_build_context}/${file}")
  }
  combined_hash_input   = join("", values(local.file_hashes))
  source_directory_hash = sha256(local.combined_hash_input)
}

resource "random_uuid" "image_tag" {
  keepers = {
    should_trigger_resource = local.source_directory_hash
  }
}

locals {
  image_tag = "${var.image_tag_prefix}-${random_uuid.image_tag.result}"
}

resource "null_resource" "build_docker_image" {
  provisioner "local-exec" {
    command     = "docker build --platform linux/amd64 --tag ${var.ecr_repository_url}:${local.image_tag} ."
    working_dir = var.docker_build_context
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }
}

resource "null_resource" "push_docker_image" {
  provisioner "local-exec" {
    command = "docker push ${var.ecr_repository_url}:${local.image_tag}"
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }

  depends_on = [null_resource.build_docker_image]
}
