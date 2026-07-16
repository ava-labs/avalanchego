#!/usr/bin/env bash

set -euo pipefail

# This script enforces two structural workflow rules:
#  1. jobs that directly use ./.github/actions/install-nix must set
#     defaults.run.shell to a nix develop invocation
#  2. once a job has that default shell, steps should not restate the exact same
#     shell value redundantly
#
# The check uses yq rather than grepping so the rule is based on workflow structure
# rather than formatting details.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflows_dir="${repo_root}/.github/workflows"
status=0

while IFS= read -r workflow; do
  relpath="${workflow#"${repo_root}/"}"
  # Find jobs that install Nix directly but do not declare a nix develop
  # default shell for their later run: steps.
  missing_defaults="$(
    # shellcheck disable=SC2016
    yq -r '
      .jobs
      | to_entries[]
      | select(any(.value.steps[]?; .uses == "./.github/actions/install-nix"))
      | select(((.value.defaults.run.shell // "") | startswith("nix develop")) | not)
      | .key
    ' "${workflow}"
  )"

  if [[ -n "${missing_defaults}" ]]; then
    while IFS= read -r job_name; do
      [[ -n "${job_name}" ]] || continue
      printf "%s: job '%s' uses install-nix but does not set defaults.run.shell: nix develop ...\n" \
        "${relpath}" \
        "${job_name}"
      status=1
    done <<< "${missing_defaults}"
  fi

  # Find step-level shell declarations that exactly duplicate the job default.
  # This keeps the redundancy check narrow: only byte-for-byte duplicates are
  # flagged, because similar-looking shell strings may still have meaningful
  # behavioral differences (for example nix develop vs nix develop --impure).
  redundant_shells="$(
    # shellcheck disable=SC2016
    yq -r '
      .jobs
      | to_entries[]
      | . as $job
      | ($job.value.defaults.run.shell // "") as $default_shell
      | select($default_shell != "")
      | ($job.value.steps // [])
      | to_entries[]
      | select((.value.shell // "") == $default_shell)
      | "\($job.key)\t\(.key)\t\($default_shell)"
    ' "${workflow}"
  )"

  if [[ -n "${redundant_shells}" ]]; then
    while IFS=$'\t' read -r job_name step_index shell_value; do
      [[ -n "${job_name}" ]] || continue
      printf "%s: job '%s' step %s redundantly sets shell: %s matching defaults.run.shell\n" \
        "${relpath}" \
        "${job_name}" \
        "${step_index}" \
        "${shell_value}"
      status=1
    done <<< "${redundant_shells}"
  fi
done < <(find "${workflows_dir}" -type f \( -name '*.yml' -o -name '*.yaml' \) | LC_ALL=C sort)

exit "${status}"
