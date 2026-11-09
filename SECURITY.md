# Security Policy

The Strettch Cloud Terraform provider authenticates to the Strettch Cloud API with a bearer API
key and manages infrastructure on your behalf. We take reports about it seriously.

## Reporting a vulnerability

**Do not open a public issue.** Report it privately through GitHub:
[Security → Report a vulnerability](https://github.com/strettch-cloud/terraform-provider-strettchcloud/security/advisories/new).

Please include:

- the affected provider version and Terraform version (`terraform version`),
- what an attacker can do and what they need first (a malicious config, a compromised API, etc.),
- steps or a proof of concept to reproduce it.

Do not include your API key or any other secret in the report.

We will acknowledge the report, keep you updated while we work on a fix, and credit you in the
advisory unless you prefer otherwise.

## Supported versions

Only the latest released version receives security fixes. Pin with a `~>` constraint and upgrade
promptly when a fix ships.

## Verifying what you install

Releases are built by GoReleaser and published to the [Terraform Registry]
(https://registry.terraform.io/providers/strettch-cloud/strettchcloud/latest), which verifies
the GPG signature over the checksums before ingesting a release. If you download the archives
directly from a GitHub release, verify them yourself:

```bash
# Check the archive against the published checksums, then verify the GPG signature.
sha256sum --check terraform-provider-strettchcloud_<version>_SHA256SUMS
gpg --verify terraform-provider-strettchcloud_<version>_SHA256SUMS.sig \
             terraform-provider-strettchcloud_<version>_SHA256SUMS
```
