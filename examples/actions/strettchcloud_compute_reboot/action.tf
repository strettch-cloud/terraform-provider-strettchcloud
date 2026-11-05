# Reboot a running compute on demand (Terraform 1.14+):
#
#   terraform apply -invoke=action.strettchcloud_compute_reboot.web

action "strettchcloud_compute_reboot" "web" {
  config {
    compute_id = strettchcloud_compute.web.id
  }
}

# ...or trigger it from a lifecycle event:
resource "strettchcloud_compute" "web" {
  host_name     = "web-01"
  image         = "UBUNTU-24.04"
  region        = "KGL-1"
  specification = "S-1C-1G"
  public_keys   = ["ssh-ed25519 AAAA..."]

  lifecycle {
    action_trigger {
      events  = [after_update]
      actions = [action.strettchcloud_compute_reboot.web]
    }
  }
}
