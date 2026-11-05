#!/usr/bin/env bash
# Every service package must ship at least one acceptance test (a TestAcc*
# function). The scoped acceptance job only *runs* the tests that exist; this
# check guarantees they *exist*, so a new resource or data source can't merge
# with only mocked unit coverage and never touch the real API.
set -euo pipefail

missing=()
for dir in internal/service/*/; do
  pkg="${dir%/}"
  if ! grep -rqsE '^func TestAcc' "${pkg}"/*_test.go; then
    missing+=("${pkg}")
  fi
done

if [ ${#missing[@]} -gt 0 ]; then
  echo "::error::Service packages with no TestAcc* acceptance test:"
  printf '  - %s\n' "${missing[@]}"
  echo "Every resource/data-source package must have at least one acceptance test"
  echo "(TestAcc*), so its code is exercised against the real API, not only mocks."
  exit 1
fi

echo "All service packages have at least one TestAcc* acceptance test."
