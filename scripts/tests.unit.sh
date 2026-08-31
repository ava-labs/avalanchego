#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
# shellcheck disable=SC1091
source "${repo_root}/scripts/constants.sh"

shuffle="-shuffle=on"
if [[ -n "${NO_SHUFFLE:-}" ]]; then
  shuffle=""
fi

race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
  race=""
fi

readonly coreth_prefix="github.com/ava-labs/avalanchego/graft/coreth"
readonly evm_prefix="github.com/ava-labs/avalanchego/graft/evm"
readonly subnet_evm_prefix="github.com/ava-labs/avalanchego/graft/subnet-evm"

goos="$(go env GOOS)"
readonly goos
packages=()
while IFS= read -r package; do
  case "${package}" in
    "${coreth_prefix}/tests"|"${coreth_prefix}/tests/"*)
      continue
      ;;
    "${evm_prefix}"|"${evm_prefix}/"*)
      ;;
    "${subnet_evm_prefix}/tests"|"${subnet_evm_prefix}/tests/"*)
      continue
      ;;
    "${coreth_prefix}"|"${coreth_prefix}/"*|"${subnet_evm_prefix}"|"${subnet_evm_prefix}/"*)
      ;;
    */mocks*|*proto*|*/tests/e2e*|*/tests/load/c*|*/tests/upgrade*|*/tests/fixture/bootstrapmonitor/e2e*|*/tests/reexecute*)
      continue
      ;;
    */tests/antithesis*)
      if [[ "${goos}" == "windows" ]]; then
        continue
      fi
      ;;
  esac
  packages+=("${package}")
done < <(
  cd "${repo_root}"
  go list ./...
  cd "${repo_root}/graft/coreth"
  go list ./...
  cd "${repo_root}/graft/evm"
  go list ./...
  cd "${repo_root}/graft/subnet-evm"
  go list ./...
)

cd "${repo_root}"
go test \
  ${shuffle:-} \
  ${race:-} \
  -timeout="${TIMEOUT:-900s}" \
  -coverprofile=coverage-all.out \
  -covermode=atomic \
  "$@" \
  "${packages[@]}"
