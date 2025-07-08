#!/usr/bin/env bash

set -euo pipefail

go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.1 "${@}"

echo "Checking use of scripts/* in GitHub Actions workflows..."
SCRIPT_USAGE=
for file in .github/workflows/*.{yml,yaml}; do
  # Skip if no matches found (in case one of the extensions doesn't exist)
  [[ -f "$file" ]] || continue

  # Search for scripts/* except for scripts/run_task.sh
  MATCHES=$(grep -H -n -P "scripts/(?!run_task\.sh)" "$file" || true)
  if [[ -n "${MATCHES}" ]]; then
    echo "${MATCHES}"
    SCRIPT_USAGE=1
  fi
done

if [[ -n "${SCRIPT_USAGE}" ]]; then
  echo "Error: the lines listed above must be converted to use scripts/run_task.sh to ensure local reproducibility."
  exit 1
fi