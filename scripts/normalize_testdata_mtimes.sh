#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
readonly timestamp=197001010000

# Go includes input-file mtimes in test-cache keys. Git checkouts assign fresh
# mtimes, so make test fixtures stable across CI runners.
while IFS= read -r -d '' testdata_dir; do
  find "${testdata_dir}" -exec touch -t "${timestamp}" {} +
done < <(find "${repo_root}" -path "${repo_root}/.git" -prune -o -type d -name testdata -print0)
