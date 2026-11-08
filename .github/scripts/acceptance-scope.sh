#!/usr/bin/env bash
# Map the files a PR changed to the Go packages whose acceptance tests cover
# them, and emit a space-separated package list as the `pkgs` GitHub Actions
# output. The acceptance job runs `go test $pkgs -run '^TestAcc'`.
#
# Scope is keyed on the package DIRECTORY, not on test-function names. A change
# confined to internal/service/<pkg>/ selects ./internal/service/<pkg>/...,
# derived from the path, so a new service package is picked up with no edit here.
#
# Any other Go or module change — shared packages, provider wiring, a brand-new
# internal/ package, main.go, go.mod/go.sum — runs the whole module, `./...`.
# This is fail-safe: the ONLY way to get a narrow run is a change that lives
# entirely under internal/service/<pkg>/; everything else runs everything.
#
# An empty output means no Go/module code changed (docs, examples, workflows,
# *.md, mise.toml, ...), so the acceptance job is skipped.
set -euo pipefail

base="${1:?usage: acceptance-scope.sh <base-ref>}"
changed="$(git diff --name-only "${base}"...HEAD)"

pkgs=()
whole=false

while IFS= read -r file; do
  [ -z "${file}" ] && continue
  case "${file}" in
    # A change confined to one service package → just that package. Derived from
    # the path (…/<pkg>/... covers testdata and other subdirs too), so a new
    # service package needs no edit here. Checked first so service files take the
    # narrow scope rather than the whole-suite catch-all below.
    internal/service/*/*)
      rest="${file#internal/service/}"   # <pkg>/...
      pkg="${rest%%/*}"                  # <pkg>
      pkgs+=("./internal/service/${pkg}/...")
      ;;
    # Any other Go or module change — shared packages, provider wiring, a new
    # internal/ package, main.go, go.mod/go.sum — can affect every resource, so
    # run the whole suite. Fail-safe default.
    *.go|go.mod|go.sum)
      whole=true
      ;;
  esac
done <<< "${changed}"

if ${whole}; then
  echo "pkgs=./..." >> "${GITHUB_OUTPUT}"
  echo "Acceptance scope: ./... (broad change)"
  exit 0
fi

if [ ${#pkgs[@]} -eq 0 ]; then
  echo "pkgs=" >> "${GITHUB_OUTPUT}"
  echo "No acceptance-relevant changes; skipping acceptance tests."
  exit 0
fi

joined="$(printf '%s\n' "${pkgs[@]}" | sort -u | paste -sd ' ' -)"
echo "pkgs=${joined}" >> "${GITHUB_OUTPUT}"
echo "Acceptance scope: ${joined}"
