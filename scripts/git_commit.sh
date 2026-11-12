#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# WARNING: this will use the most recent commit even if there are un-committed changes present
git_commit="${AVALANCHEGO_COMMIT:-$(git --git-dir="${AVALANCHE_PATH}/.git" rev-parse HEAD)}"
commit_hash="${git_commit::8}"

# Source constants.sh first to include static_ld_flags.
go_ldflags="-X github.com/ava-labs/avalanchego/version.GitCommit=${git_commit} ${static_ld_flags:-}"
