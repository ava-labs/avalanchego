#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
"${AVALANCHE_PATH}"/scripts/run_tool.sh actionlint "${@}"

echo "Checking use of scripts/* in GitHub Actions workflows..."
SCRIPT_USAGE=
for file in "${AVALANCHE_PATH}"/.github/workflows/*.{yml,yaml}; do
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

echo "Checking for floating GitHub runner labels..."
# Floating runner labels (e.g. ubuntu-latest, macos-latest) can change out
# from under the default branch and break CI without any corresponding change
# in the repository. Require explicit runner versions so image upgrades happen
# only through intentional repo changes that can be reviewed.
FLOATING_RUNNERS=
for file in "${AVALANCHE_PATH}"/.github/workflows/*.{yml,yaml,json}; do
  # Skip if no matches found (in case one of the extensions doesn't exist)
  [[ -f "$file" ]] || continue

  MATCHES=$(grep -H -n -P '\b(?:ubuntu|macos)-latest\b' "$file" || true)
  if [[ -n "${MATCHES}" ]]; then
    echo "${MATCHES}"
    FLOATING_RUNNERS=1
  fi
done

if [[ -n "${FLOATING_RUNNERS}" ]]; then
  echo "Error: floating GitHub runner labels are forbidden."
  echo "Pin explicit runner versions instead so runner image upgrades happen through reviewed repo changes rather than when GitHub updates a floating label on the default branch."
  exit 1
fi
