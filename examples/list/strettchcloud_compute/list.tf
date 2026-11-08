# computes.tfquery.hcl — discover every compute in the team and bulk-import them.
# Requires Terraform 1.14+.
#
#   terraform query                              # print every compute with its identity
#   terraform query -generate-config-out=all.tf  # write resource + import blocks
#   terraform apply                              # import them; nothing is created

list "strettchcloud_compute" "all" {
  provider = strettchcloud
}
