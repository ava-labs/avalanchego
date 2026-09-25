#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
readonly timestamp=197001010000

# Stable fixture mtimes let an exact GOCACHE restore reuse test results from
# another CI runner. See docs/ci.md#stable-fixture-modification-times.
while IFS= read -r -d '' testdata_dir; do
  find "${testdata_dir}" -exec touch -t "${timestamp}" {} +
done < <(find "${repo_root}" -path "${repo_root}/.git" -prune -o -type d -name testdata -print0)
