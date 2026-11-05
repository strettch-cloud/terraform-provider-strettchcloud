# List every OS image in the catalog.
data "strettchcloud_images" "all" {}

# All image slugs, e.g. for a dropdown or validation.
output "all_slugs" {
  value = data.strettchcloud_images.all.images[*].image
}

# Every version offered for a given image name.
output "ubuntu_versions" {
  value = [
    for img in data.strettchcloud_images.all.images :
    img.version if img.image_name == "Ubuntu"
  ]
}
