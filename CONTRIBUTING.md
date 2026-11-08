# Contributing to the Strettch Cloud Terraform provider

Thanks for helping. Bug reports, fixes and improvements are all welcome.

## Before you start

- **Security issues** go through [private reporting](SECURITY.md), never a public issue.
- For anything larger than a small fix, open an issue first so we can agree on the approach.
- By contributing you agree that your work is licensed under the [Apache License 2.0](LICENSE),
  and you follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

The toolchain (Go, golangci-lint, goreleaser, Terraform, Trivy, govulncheck, syft) is pinned
in `mise.toml`. CI runs the same `mise run` tasks.

```bash
# Install mise: https://mise.jdx.dev
mise install            # installs Go 1.27 and the pinned tools
mise tasks              # list every task
mise run install-hooks  # enable the pre-commit hook (polin-guard + lint); once per clone
```

### The standard loop

```bash
# 1. Free checks: lint, tidy, unit + client tests with coverage. No key, seconds.
mise run lint
mise run tidy-check
mise run test-coverage

# 2. Regenerate the docs from the schema and examples; commit the result.
mise run docs

# 3. Acceptance tests with the dedicated sandbox team's key (no URL needed; the
#    provider defaults to the production Public API). Creates and destroys real computes.
export STRETTCH_CLOUD_API_KEY=<sandbox team key>
mise run sweep && mise run test-acceptance-coverage
```

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Use a [Conventional Commits](https://www.conventionalcommits.org) title, e.g.
   `fix: tolerate the pre-reboot RUNNING state when polling`. Pull requests are squash-merged,
   so the title becomes the commit message — and release notes are generated from these titles,
   so write them for a reader (`feat:`/`fix:` are grouped; `docs:`/`test:`/`chore:` are omitted).
3. Open the pull request against `main`. CI runs lint, unit tests, and the security scans on
   every PR. Acceptance tests run only when a maintainer adds the `run-ci` label.

Changes to `.github/`, `.goreleaser.yml`, `go.mod`/`go.sum` and `mise.toml` affect how releases
are built and published, so they always need a maintainer's review (see
[`.github/CODEOWNERS`](.github/CODEOWNERS)). Releases are cut by maintainers from a `vX.Y.Z`
tag; GoReleaser then builds the signed archives the Terraform Registry ingests.
