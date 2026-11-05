# Look up one region by slug.
data "strettchcloud_region" "kgl" {
  region = "KGL-1"
}

# List every region in the catalog.
data "strettchcloud_regions" "all" {}
