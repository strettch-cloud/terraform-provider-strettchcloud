# Import an existing compute by its id. The first apply after import plans an
# in-place update on public_keys (never a replace); set public_keys in config to
# match the key the compute was created with.
terraform import strettchcloud_compute.web 325
