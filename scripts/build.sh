#!/usr/bin/env bash

#
# Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.
#

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build avalanchego

  Options:

    -r  Build with race detector
"
}

race=''
while getopts 'r' flag; do
  case "${flag}" in
    r)
      echo "Building with race detection enabled"
      race='-race'
      ;;
    *) print_usage
      exit 1 ;;
  esac
done

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Configure the build environment
source "${REPO_ROOT}"/scripts/constants.sh
# Determine the git commit hash to use for the build
source "${REPO_ROOT}"/scripts/git_commit.sh

echo "Building AvalancheGo with [$(go version)]..."
go build ${race} -o "${avalanchego_path}" \
   -ldflags "-X github.com/ava-labs/avalanchego/version.GitCommit=$git_commit $static_ld_flags" \
   "${REPO_ROOT}"/main
