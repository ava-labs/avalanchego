#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
readonly module_directories=(
  .
  graft/coreth
  graft/evm
  graft/subnet-evm
  tools/external
)

for module_directory in "${module_directories[@]}"; do
  (
    cd "${repo_root}/${module_directory}"
    GOWORK=off go mod download all
  )
done
