#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Where AvalancheGo binary goes
avalanchego_path="$AVALANCHE_PATH/build/avalanchego"

# Image tag based on current branch  (shared between image build and its test script)
# TODO: fix "fatal: No names found, cannot describe anything" in github CI
image_tag=$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true)
if [[ -z $image_tag ]]; then
  # Supply a default tag when one is not discovered
  image_tag=ci_dummy
elif [[ "$image_tag" == */* ]]; then
  # Slashes are not legal for docker image tags - replace with dashes
  image_tag=$(echo "$image_tag" | tr '/' '-')
fi

# Current commit (shared between image build and its test script)
# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$AVALANCHE_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}

# Static compilation
static_ld_flags=''
if [ "${STATIC_COMPILATION:-}" = 1 ]
then
    export CC=musl-gcc
    which $CC > /dev/null || ( echo $CC must be available for static compilation && exit 1 )
    static_ld_flags=' -extldflags "-static" -linkmode external '
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
# While CGO_ENABLED doesn't need to be explicitly set, it produces a much more
# clear error due to the default value change in go1.20.
export CGO_ENABLED=1

# Disable version control fallbacks
export GOPROXY="https://proxy.golang.org"
