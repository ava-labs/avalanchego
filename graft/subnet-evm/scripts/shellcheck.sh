#!/usr/bin/env bash

set -euo pipefail

# Scripts that are sourced from upstream and not maintained in this repo will not be shellchecked.
# Also ignore the local avalanchego clone, git submodules, and node_modules.
IGNORED_FILES="
  metrics/validate.sh
  avalanchego/*
  contracts/lib/*
  contracts/node_modules/*
"

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

IGNORED_CONDITIONS=()
for file in ${IGNORED_FILES}; do
  if [[ -n "${IGNORED_CONDITIONS-}" ]]; then
    IGNORED_CONDITIONS+=(-o)
  fi
  IGNORED_CONDITIONS+=(-path "${REPO_ROOT}/${file}" -prune)
done

find "${REPO_ROOT}" \( "${IGNORED_CONDITIONS[@]}" \) -o -type f -name "*.sh" -print0 | xargs -0 shellcheck "${@}"
