#!/usr/bin/env bash
set -euo pipefail

# E2E-cache policy. Unit-only configuration must not invalidate this archive.
repo_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
exec "$(dirname "${BASH_SOURCE[0]}")/workflow-hash-go-cache-inputs.sh" "${repo_root}" \
  taskfiles/go-e2e.yml \
  .github/actions/install-nix/action.yml .github/actions/run-monitored-tmpnet-cmd/action.yml \
  .github/actions/validate-go-e2e-cache/action.yml \
  scripts/build.sh scripts/build_xsvm.sh scripts/tests.e2e.sh scripts/run_ginkgo.sh \
  scripts/git_commit.sh scripts/workflow-check-go-e2e-cache.sh \
  scripts/workflow-validate-go-build-cache.sh scripts/workflow-hash-go-e2e-inputs.sh
