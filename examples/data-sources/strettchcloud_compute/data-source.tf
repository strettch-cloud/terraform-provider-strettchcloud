# Look up one compute by its id.
data "strettchcloud_compute" "web" {
  id = "325"
}

output "web_ip" {
  value = data.strettchcloud_compute.web.ipv4
}
