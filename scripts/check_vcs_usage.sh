#!/usr/bin/env bash

# Detects direct git or jj commands in shell scripts that should use
# scripts/vcs.sh instead. Whitelisted lines are marked with a
# "# vcs-ok: <reason>" comment explaining why direct usage is acceptable.
#
# Usage: ./scripts/check_vcs_usage.sh

set -euo pipefail

if ! [[ "$0" =~ scripts/check_vcs_usage.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Files that implement or test the VCS abstraction layer itself.
EXCLUDED_FILES=(
  scripts/vcs.sh
  scripts/tests.vcs.sh
)

# Find all .sh files. Include .github/ scripts (dotfiles are missed by
# `find *`, so we list .github explicitly).
#
# shellcheck disable=SC2035
mapfile -t scripts < <(
  {
    find * -name '*.sh' -type f -print
    find .github -name '*.sh' -type f 2>/dev/null || true
  } | sort -u
)
if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "Error: no .sh files found (are you in the repository root?)" >&2
  exit 1
fi

# Also find Taskfile.yml files whose sh: blocks may contain git/jj usage.
# shellcheck disable=SC2035
mapfile -t taskfiles < <(
  {
    find * -name 'Taskfile.yml' -type f -print
    find .github -name 'Taskfile.yml' -type f 2>/dev/null || true
  } | sort -u
)
if [[ ${#taskfiles[@]} -eq 0 ]]; then
  echo "Error: no Taskfile.yml files found (are you in the repository root?)" >&2
  exit 1
fi

# Match lines that invoke git or jj as a command. The pattern matches:
#   - Start of line or after shell operators (|, &&, ||, ;, $( , `, !)
#   - Optional leading whitespace
#   - The literal word "git" or "jj" followed by a space or end of line
#
# Lines containing "# vcs-ok:" are whitelisted.
violations=0

for script in "${scripts[@]}"; do
  # Skip excluded files
  skip=false
  for excluded in "${EXCLUDED_FILES[@]}"; do
    if [[ "$script" == "$excluded" ]]; then
      skip=true
      break
    fi
  done
  if [[ "$skip" == true ]]; then
    continue
  fi

  # Read the file and check each line. A "# vcs-ok:" comment on the
  # same line OR on the immediately preceding line whitelists a match.
  # The preceding-line form is useful when the git/jj invocation is in
  # the middle of a multi-line string (e.g., a commit message).
  line_num=0
  prev_line=""
  while IFS= read -r line; do
    line_num=$((line_num + 1))

    # Skip whitelisted lines (same line or preceding line)
    if [[ "$line" == *"# vcs-ok:"* ]] || [[ "$prev_line" == *"# vcs-ok:"* ]]; then
      prev_line="$line"
      continue
    fi

    # Skip pure comment lines (no code before the #)
    stripped="${line#"${line%%[![:space:]]*}"}"
    if [[ "$stripped" == \#* ]]; then
      prev_line="$line"
      continue
    fi

    # Check for direct git or jj command invocations
    if echo "$line" | grep -qE '(^|[|;&`(! ])[ \t]*(git|jj)( |$)'; then
      echo "${script}:${line_num}: ${line}"
      violations=$((violations + 1))
    fi
    prev_line="$line"
  done < "$script"
done

# Check Taskfile.yml sh: blocks for direct git/jj usage. Scans lines
# the same way as shell scripts; the vcs-ok whitelist handles any false
# positives from non-shell YAML content.
for taskfile in "${taskfiles[@]}"; do
  line_num=0
  prev_line=""
  while IFS= read -r line; do
    line_num=$((line_num + 1))

    # Skip whitelisted lines (same line or preceding line)
    if [[ "$line" == *"# vcs-ok:"* ]] || [[ "$prev_line" == *"# vcs-ok:"* ]]; then
      prev_line="$line"
      continue
    fi

    # Skip YAML comment lines
    stripped="${line#"${line%%[![:space:]]*}"}"
    if [[ "$stripped" == \#* ]]; then
      prev_line="$line"
      continue
    fi

    if echo "$line" | grep -qE '(^|[|;&`(! ])[ \t]*(git|jj)( |$)'; then
      echo "${taskfile}:${line_num}: ${line}"
      violations=$((violations + 1))
    fi
    prev_line="$line"
  done < "$taskfile"
done

if [[ $violations -gt 0 ]]; then
  echo ""
  echo "${violations} violation(s) found."
  echo ""
  echo "Scripts should use scripts/vcs.sh functions instead of direct git/jj commands."
  # vcs-ok: error message explaining the policy
  echo "This ensures that as much tooling as possible is compatible with jj workspaces, which lack a .git directory."
  echo "If direct usage is intentional, add a '# vcs-ok: <reason>' comment to the line."
  exit 1
fi

echo "No unwhitelisted git/jj usage found."
