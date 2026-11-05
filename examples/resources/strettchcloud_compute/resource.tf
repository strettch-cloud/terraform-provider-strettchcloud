data "strettchcloud_specification" "medium" {
  vcpus     = 2
  memory_mb = 4096
}

resource "strettchcloud_compute" "web" {
  host_name      = "web-01"
  image          = "UBUNTU-24.04"
  region         = "KGL-1"
  specification  = data.strettchcloud_specification.medium.specification
  public_keys    = ["ssh-ed25519 AAAA..."]
  with_public_ip = true
  tags           = ["production", "web"]
  # state defaults to "RUNNING"; set "STOPPED" to keep it powered off.
}

output "ip" {
  value = strettchcloud_compute.web.ipv4
}
