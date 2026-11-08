# Changelog

All notable changes to this provider are documented here, one entry per release.
New entries land under `Unreleased` as PRs merge; a release moves them under its
version heading.

## Unreleased

### Added

- Initial provider: `provider "strettchcloud"` with `api_key` / `api_url` and
  rate-limit and retry tuning.
- Resource `strettchcloud_compute`: create, read, update (host_name, tags,
  specification resize, power state), delete, and import.
- Data sources `strettchcloud_specification` / `strettchcloud_specifications`,
  `strettchcloud_image` / `strettchcloud_images`,
  `strettchcloud_region` / `strettchcloud_regions`, and `strettchcloud_compute`
  (by id).
- Resource identity and the `strettchcloud_compute` list resource for
  `terraform query`-based discovery and bulk import (Terraform 1.14+).
- Action `strettchcloud_compute_reboot` (Terraform 1.14+).
