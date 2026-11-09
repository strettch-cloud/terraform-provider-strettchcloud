# Reboot a running compute (Terraform 1.14+). Actions run from a resource's
# lifecycle events — wire the reboot to a trigger such as after_update, then it
# runs on the next `terraform apply` that updates the compute.

action "strettchcloud_compute_reboot" "web" {
  config {
    compute_id = strettchcloud_compute.web.id
  }
}

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
