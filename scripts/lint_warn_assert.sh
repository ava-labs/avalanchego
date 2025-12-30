#!/usr/bin/env bash
set -euo pipefail
#
# lint_warn_assert.sh - Shared helper for testify/assert advisory warnings
#
# Usage:
#   source /path/to/lint_warn_assert.sh
#   test_warn_testify_assert

# Advisory check for testify/assert usage - warns but doesn't fail.
# Only runs on CI PRs, checking new code only.
# assert continues execution after failure, require fails fast.
# Developers should consciously choose when assert is appropriate.
#
# To run locally: WARN_TESTIFY_ASSERT=1 TESTS='warn_testify_assert' ./scripts/lint.sh
function test_warn_testify_assert {
  local root_dir
  root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")"; cd .. && pwd)"
  local config_path="${root_dir}/.golangci-warn-assert.yml"
  local tools_mod="${root_dir}/tools/go.mod"

  local args=(
    --config "$config_path"
    --issues-exit-code=0
  )

  if [[ -n "${GITHUB_BASE_REF:-}" ]]; then
    # In a PR: fetch base branch and only check new code
    git fetch origin "${GITHUB_BASE_REF}" --depth=1 2>/dev/null || true
    args+=(--new-from-rev="origin/${GITHUB_BASE_REF}")
  elif [[ -z "${WARN_TESTIFY_ASSERT:-}" ]]; then
    echo "Skipping (only runs on CI PRs or with WARN_TESTIFY_ASSERT=1)"
    return 0
  fi

  # Run golangci-lint and transform output to GitHub warning annotations
  local output
  output=$(go tool -modfile="$tools_mod" golangci-lint run "${args[@]}" 2>&1) || true

  if [[ -z "$output" ]]; then
    return 0
  fi

  # In GitHub Actions, emit ::warning annotations (not raw output which shows confusing "Error:" lines)
  # For local runs, show the raw output
  #
  # Note: GitHub annotations don't support multiline messages, so we use a short message to avoid a horizontal scroll.
  # See: https://github.com/actions/toolkit/issues/193
  #      https://github.com/orgs/community/discussions/122594
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "$output" | grep -E '^[^:]+:[0-9]+:[0-9]+:' | while IFS= read -r line; do
      # Parse "file:line:col: message"
      local file line_num col msg
      file=$(echo "$line" | cut -d: -f1)
      line_num=$(echo "$line" | cut -d: -f2)
      col=$(echo "$line" | cut -d: -f3)
      # Clean up forbidigo's boilerplate phrasing
      msg=$(echo "$line" | cut -d: -f4- \
        | sed 's/^ *//' \
        | sed 's/^use of //' \
        | sed 's/ forbidden because "/" /' \
        | sed 's/" (forbidigo)$//')
      echo "::warning file=${file},line=${line_num},col=${col}::${msg}"
    done || true  # grep returns 1 when no matches, which is fine
  else
    echo "$output"
  fi
}
