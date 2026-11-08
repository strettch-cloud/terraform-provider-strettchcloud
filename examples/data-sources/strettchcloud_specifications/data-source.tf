# List every compute specification in the catalog.
data "strettchcloud_specifications" "all" {}

# All specification slugs, e.g. for a dropdown or validation.
output "all_slugs" {
  value = data.strettchcloud_specifications.all.specifications[*].specification
}

# Pick a specification by exact size without hardcoding its slug.
output "medium_slug" {
  value = one([
    for s in data.strettchcloud_specifications.all.specifications :
    s.specification if s.vcpus == 2 && s.memory_mb == 4096
  ])
}
