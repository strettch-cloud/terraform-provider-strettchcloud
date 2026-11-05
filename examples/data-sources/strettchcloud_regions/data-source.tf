# List every region in the catalog.
data "strettchcloud_regions" "all" {}

# All region slugs, e.g. for a dropdown or validation.
output "all_slugs" {
  value = data.strettchcloud_regions.all.regions[*].region
}

# Region slugs in a given country.
output "rwanda_regions" {
  value = [
    for r in data.strettchcloud_regions.all.regions :
    r.region if r.country_code == "RW"
  ]
}
