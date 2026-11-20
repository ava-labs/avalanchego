#!/usr/bin/env bash
set -euo pipefail

# Unit-cache policy. E2E-only configuration must not invalidate this archive.
repo_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
exec "$(dirname "${BASH_SOURCE[0]}")/workflow-hash-go-cache-inputs.sh" "${repo_root}" \
  taskfiles/go-unit.yml .github/workflows/go-unit.yml \
  scripts/tests.unit.sh scripts/normalize_testdata_mtimes.sh \
  scripts/workflow-prepare-go-unit-cache.sh scripts/workflow-hash-go-unit-inputs.sh
