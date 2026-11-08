terraform {
  required_providers {
    strettchcloud = {
      source  = "strettch-cloud/strettchcloud"
      version = "~> 1.0"
    }
  }
}

provider "strettchcloud" {
  # api_key defaults to the STRETTCH_CLOUD_API_KEY environment variable.
}

# Provision a compute instance.
resource "strettchcloud_compute" "web" {
  host_name      = "web-01"
  image          = "UBUNTU-24.04"
  region         = "KGL-1"
  specification  = "S-1C-2G"
  public_keys    = ["ssh-ed25519 AAAA..."]
  with_public_ip = true
}
