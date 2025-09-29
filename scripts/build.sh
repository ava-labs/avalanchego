#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build avalanchego

  Options:

    -r              Build with race detector
    -f VERSION      Build with Firewood FFI
                    VERSION format: ffi/vX.Y.Z for pre-built, commit/branch for source
"
}

race=''
firewood_version=''

while getopts 'rf:' flag; do
  case "${flag}" in
    r)
      echo "Building with race detection enabled"
      race='-race'
      ;;
    f)
      firewood_version="${OPTARG}"
      echo "Building with Firewood version: ${firewood_version}"
      ;;
    *)
      print_usage
      exit 1
      ;;
  esac
done

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Configure the build environment
source "${REPO_ROOT}"/scripts/constants.sh
# Determine the git commit hash to use for the build
source "${REPO_ROOT}"/scripts/git_commit.sh

if [ -n "${firewood_version}" ]; then
  "${REPO_ROOT}/scripts/setup_firewood.sh" "${firewood_version}" "${REPO_ROOT}"
fi

echo "Building AvalancheGo with [$(go version)]..."
go build ${race} -o "${avalanchego_path}" \
   -ldflags "-X github.com/ava-labs/avalanchego/version.GitCommit=$git_commit $static_ld_flags" \
   "${REPO_ROOT}"/main
