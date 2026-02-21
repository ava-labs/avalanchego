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

# find_files discovers files matching the given pattern, including under
# .github/ which is missed by `find *`.
find_files() {
  local pattern="$1"
  # shellcheck disable=SC2035
  {
    find * -name "$pattern" -type f -print
    find .github -name "$pattern" -type f 2>/dev/null || true
  } | sort -u
}

# check_files scans files for direct git/jj command invocations.
# A "# vcs-ok:" comment on the same line OR on the immediately preceding
# line whitelists a match (useful for multi-line strings like commit
# messages).
#
# Returns the number of violations found via the global violations counter.
violations=0

check_files() {
  local -n files=$1

  for file in "${files[@]}"; do
    # Skip excluded files
    local skip=false
    for excluded in "${EXCLUDED_FILES[@]}"; do
      if [[ "$file" == "$excluded" ]]; then
        skip=true
        break
      fi
    done
    if [[ "$skip" == true ]]; then
      continue
    fi

    local line_num=0
    local prev_line=""
    while IFS= read -r line; do
      line_num=$((line_num + 1))

      # Skip whitelisted lines (same line or preceding line)
      if [[ "$line" == *"# vcs-ok:"* ]] || [[ "$prev_line" == *"# vcs-ok:"* ]]; then
        prev_line="$line"
        continue
      fi

      # Skip pure comment lines (no code before the #)
      local stripped="${line#"${line%%[![:space:]]*}"}"
      if [[ "$stripped" == \#* ]]; then
        prev_line="$line"
        continue
      fi

      if echo "$line" | grep -qE '(^|[|;&`(! ])[ \t]*(git|jj)( |$)'; then
        echo "${file}:${line_num}: ${line}"
        violations=$((violations + 1))
      fi
      prev_line="$line"
    done < "$file"
  done
}

# Collect files to scan.
mapfile -t scripts < <(find_files '*.sh')
if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "Error: no .sh files found" >&2
  exit 1
fi

mapfile -t taskfiles < <(find_files 'Taskfile.yml')
if [[ ${#taskfiles[@]} -eq 0 ]]; then
  echo "Error: no Taskfile.yml files found" >&2
  exit 1
fi

mapfile -t dockerfiles < <(find_files 'Dockerfile*')
if [[ ${#dockerfiles[@]} -eq 0 ]]; then
  echo "Error: no Dockerfiles found" >&2
  exit 1
fi

# Scan all file types for violations.
check_files scripts
check_files taskfiles
check_files dockerfiles

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
