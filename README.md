# terraform-provider-strettchcloud

The Strettch Cloud Terraform provider lets you manage your Strettch Cloud
infrastructure through the
[Strettch Cloud API](https://cloud.strettch.com/docs/api-reference/introduction).

## Using the provider

```hcl
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
```

Set your API key:

```shell
export STRETTCH_CLOUD_API_KEY="<your api key>"
```

See the [registry documentation](https://registry.terraform.io/providers/strettch-cloud/strettchcloud/latest/docs)
for the full resource and data-source reference.

### Requirements

- **Terraform** — any current version for the core resource and data sources;
  **Terraform 1.14+** for Terraform Search (list resources) and the reboot action.
- **Go 1.27** and **mise** to build from source (below).

## License

Apache License 2.0. See [LICENSE](./LICENSE).
