#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_modules.sh"

cd "${REPO_ROOT}"

# GOWORK="" ensures workspace mode is enabled regardless of the caller's
# environment. Every command below must write to go.work.sum, which only
# exists in workspace mode.
export GOWORK=""

go work sync

# go work sync records only the checksums that resolve the workspace module
# graph. It does not record the checksums that build and test the packages in
# that graph. The go command does not add them later either. It finds them in
# the member go.sum files, and it writes to go.work.sum only for checksums it
# cannot find there. See https://github.com/golang/go/issues/63901.
#
# Per-module commands read the member go.sum, so they do not show the gap. A
# workspace-wide `go test ./... ./graft/...` reads go.work.sum alone. It fails
# with 'missing go.sum entry'.
#
# The steps below move the member go.sum files aside. They then load every
# package and test dependency in the workspace. The go command has no member
# go.sum to read, so it writes each checksum it needs into go.work.sum.
#
# Build constraints select different files per platform. Those files pull in
# different modules. One load per supported platform is therefore necessary.
# See docs/ci.md for the rejected alternatives and the removal criterion.
readonly PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/arm64"
)
go_sums=("${GO_MODS[@]/%go.mod/go.sum}")
hidden_dir="$(mktemp -d)"

restore_go_sums() {
  local i
  for i in "${!go_sums[@]}"; do
    if [[ -f "${hidden_dir}/${i}" ]]; then
      mv "${hidden_dir}/${i}" "${go_sums[${i}]}"
    fi
  done
  rmdir "${hidden_dir}" 2>/dev/null || true
}
trap restore_go_sums EXIT INT TERM

for i in "${!go_sums[@]}"; do
  mv "${go_sums[${i}]}" "${hidden_dir}/${i}"
done

package_patterns=()
for prefix in "${TAG_PREFIXES[@]}"; do
  package_patterns+=("./${prefix}...")
done

go list -m all > /dev/null
for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}" GOARCH="${platform#*/}" \
    go list -deps -test "${package_patterns[@]}" > /dev/null
done
