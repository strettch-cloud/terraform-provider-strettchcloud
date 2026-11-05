# Look up one specification by slug...
data "strettchcloud_specification" "by_slug" {
  specification = "S-1C-1G"
}

# ...or by size. Filters combine with AND and must match exactly one entry.
data "strettchcloud_specification" "by_size" {
  vcpus     = 2
  memory_mb = 4096
}

# List every specification in the catalog.
data "strettchcloud_specifications" "all" {}

output "all_slugs" {
  value = data.strettchcloud_specifications.all.specifications[*].specification
}
