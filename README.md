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

## Developing the provider

All tooling is pinned in [`mise.toml`](./mise.toml); `mise` installs Go 1.27 and
everything else. CI runs the same `mise run` tasks.

```shell
mise install        # installs Go 1.27 and the pinned tools
```

### The standard loop

```shell
# 1. Free checks: lint, tidy, unit + client tests with coverage. No key, seconds.
mise run lint
mise run tidy-check
mise run test-coverage

# 2. Regenerate the docs from the schema and examples; commit the result.
mise run docs

# 3. Acceptance tests with the dedicated production sandbox team's key.
#    No URL needed; the provider defaults to the production API.
export STRETTCH_CLOUD_API_KEY=<production sandbox team key>
mise run sweep && mise run test-acceptance-coverage
```

Run all three before pushing. Forgetting `mise run docs` fails the PR. The
acceptance suite creates and destroys real computes (smallest specification, tagged `tf-acc-test`); `mise run sweep` first
deletes leftovers from an earlier crashed run. Open `coverage.html` to see which lines a change
left untested; both patch coverage (new/changed lines) and overall project
coverage must stay at or above 98%.

### Useful tasks

| Task | What it does |
|---|---|
| `mise run lint` | golangci-lint |
| `mise run test` | unit + client tests (no key) |
| `mise run test-coverage` | the above, with a coverage report |
| `mise run test-acceptance` | full acceptance suite against the production sandbox |
| `mise run sweep` | delete leftover `tf-acc-test` computes |
| `mise run docs` | regenerate `docs/` with tfplugindocs |
| `mise run docs-check` | fail if `docs/` is stale |
| `mise run snapshot` | cross-compile every release platform |

## License

Apache License 2.0. See [LICENSE](./LICENSE).
