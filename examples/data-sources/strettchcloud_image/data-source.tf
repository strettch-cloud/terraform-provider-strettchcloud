# Look up one image by slug...
data "strettchcloud_image" "by_slug" {
  image = "UBUNTU-24.04"
}

# ...or by name and version. Filters combine with AND and must match exactly one.
data "strettchcloud_image" "by_name" {
  image_name = "Ubuntu"
  version    = "24.04"
}

# List every image in the catalog.
data "strettchcloud_images" "all" {}
