#!/usr/bin/env bash

# lint_setup.sh - Shared linting configuration setup
#
# Exports:
#   AVALANCHE_FILES - Array of Go files created by Avalanche to be linted
#   UPSTREAM_FILES - Array of Go files adapted from go-ethereum to be linted
#   AVALANCHE_LINT_FILE - Path to temporary avalanche-specific lint config
#
# Usage:
#   source ./scripts/lint_setup.sh
#   setup_lint
# This script function must be run from the repository root.

set -euo pipefail

# Read excluded directories into arrays
AVALANCHE_FILES=()
UPSTREAM_FILES=()
AVALANCHE_LINT_FILE=""
function setup_lint {
  # If this has already been called, we don't need to create another file.
  if [ -n "$AVALANCHE_LINT_FILE" ]; then
    return
  fi

  local upstream_folders_file="./scripts/upstream_files.txt"
  # Read the upstream_folders file into an array
  mapfile -t upstream_folders <"$upstream_folders_file"

  # Shared find filters
  local -a find_filters=(
    -type f
    -name '*.go'
    ! -name '*.pb.go'
    ! -name 'mock_*.go'
    ! -name 'mocks_*.go'
    ! -name 'mocks.go'
    ! -name 'mock.go'
    ! -name 'gen_*.go'
    ! -path './**/*mock/*.go'
  )

  # Combined loop: build both upstream licensed find and exclude args
  local -a upstream_find_args=()
  local -a upstream_exclude_args=()
  for line in "${upstream_folders[@]}"; do
    # Skip empty lines
    [[ -z "$line" ]] && continue

    if [[ "$line" == !* ]]; then
      # Excluding files with !
      upstream_exclude_args+=(! -path "./${line:1}")
    else
      upstream_find_args+=(-path "./${line}" -o)
    fi
  done
  # Remove the last '-o' from the arrays
  unset 'upstream_find_args[${#upstream_find_args[@]}-1]'

  # Find upstream files
  # shellcheck disable=SC2034  # used by external scripts after sourcing
  mapfile -t UPSTREAM_FILES < <(
    find . \
      \( "${upstream_find_args[@]}" \) \
      -a \( "${find_filters[@]}" \) \
      "${upstream_exclude_args[@]}"
  )

  # Build exclusion args from upstream files
  default_exclude_args=()
  for f in "${UPSTREAM_FILES[@]}"; do
    default_exclude_args+=(! -path "$f")
  done

  # Now find default files (exclude already licensed ones)
  # shellcheck disable=SC2034  # used by external scripts after sourcing
  mapfile -t AVALANCHE_FILES < <(find . "${find_filters[@]}" "${default_exclude_args[@]}")

  # copy avalanche file to temp directory to edit
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf -- "$TMP_DIR"' EXIT

  AVALANCHE_LINT_FILE="${TMP_DIR}/.golangci.yml"
  cp .golangci.yml "$AVALANCHE_LINT_FILE"

  # Exclude all upstream files dynamically
  echo "    paths-except:" >> "$AVALANCHE_LINT_FILE"
  for f in "${UPSTREAM_FILES[@]}"; do
    # exclude pre-pended "./"
    echo "      - \"${f:2}\$\"" >> "$AVALANCHE_LINT_FILE"
  done
}
