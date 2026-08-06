#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
"${AVALANCHE_PATH}"/scripts/run_tool.sh actionlint "${@}"

echo "Checking use of scripts/* in GitHub Actions workflows..."
SCRIPT_USAGE=
for file in "${AVALANCHE_PATH}"/.github/workflows/*.{yml,yaml}; do
  # Skip if no matches found (in case one of the extensions doesn't exist)
  [[ -f "$file" ]] || continue

  # Search for scripts/* except for:
  #   - scripts/run_task.sh (the approved launcher for developer entrypoints)
  #   - workflow-*.sh       (CI-only glue scripts, not developer entrypoints)
  MATCHES=$(grep -H -n -P "scripts/(?!run_task\.sh|workflow-)" "$file" || true)
  if [[ -n "${MATCHES}" ]]; then
    echo "${MATCHES}"
    SCRIPT_USAGE=1
  fi
done

if [[ -n "${SCRIPT_USAGE}" ]]; then
  echo "Error: the lines listed above must be converted to use scripts/run_task.sh to ensure local reproducibility."
  echo "       CI-only helpers may use the workflow-*.sh naming convention to bypass this check."
  exit 1
fi

echo "Checking for workflow task invocations configured with passthrough flags..."
# CI should invoke stable named tasks rather than redefining task behavior at
# the workflow callsite. As a practical, best-effort guardrail, reject task
# invocations that pass extra option flags after `--`.
#
# This check is intentionally narrow. It is meant to catch a common failure
# mode, not to exhaustively model every possible workflow command form.
TASK_CONFIGURATION=
for file in "${AVALANCHE_PATH}"/.github/workflows/*.{yml,yaml}; do
  [[ -f "$file" ]] || continue

  MATCHES=$(grep -H -n -P '^\s*run:\s*(?:(?:\./)?scripts/run_task\.sh|task)\s+[^#\n]*\s--\s+--' "$file" || true)
  if [[ -n "${MATCHES}" ]]; then
    echo "${MATCHES}"
    TASK_CONFIGURATION=1
  fi
done

if [[ -n "${TASK_CONFIGURATION}" ]]; then
  echo "Error: workflow task invocations must not pass option flags after '--'."
  echo "Define a stable named task instead of configuring task behavior at the CI callsite."
  exit 1
fi

echo "Checking third-party action refs are pinned and have target tags..."
# Allow floating major tags only for actions/*, which we already have to trust
# as part of the GitHub Actions platform. Other third-party actions must use a
# full commit SHA and a target-tag comment so upgrades are explicit and reviewable.
#
# Reject: uses: aws-actions/configure-aws-credentials@v6
# Accept: uses: aws-actions/configure-aws-credentials@e6de054238d6b7531b4efff3b6587d9aade6a06c # v6
FLOATING_ACTION_REFS=
if FLOATING_ACTION_REFS=$(rg --hidden -n -P '^\s*(?:-\s*)?uses:\s+(?!\.?/)(?!actions/)[^@\s]+@(?!(?:[0-9a-fA-F]{40})\s+#\s+\S+\s*$)' "${AVALANCHE_PATH}/.github" -g '*.yml' -g '*.yaml'); then
  :
else
  RG_EXIT_CODE=$?
  if [[ ${RG_EXIT_CODE} -ne 1 ]]; then
    echo "Error: failed to search GitHub Actions configuration for floating action refs."
    exit "${RG_EXIT_CODE}"
  fi
fi

if [[ -n "${FLOATING_ACTION_REFS}" ]]; then
  echo "${FLOATING_ACTION_REFS}"
  echo "Error: only actions/* may use floating tags in GitHub Actions configuration."
  echo "Pin every other third-party action to a full commit SHA with a # <tag> comment."
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
