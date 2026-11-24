#!/usr/bin/env bash

set -euo pipefail

# Builds an avalanchego docker image against Firewood built from source.
#
# e.g.,
# FIREWOOD_REF=main ./scripts/build_firewood_image.sh                                      # Build local image
# FIREWOOD_REF=main DOCKER_IMAGE=avaplatform/avalanchego ./scripts/build_firewood_image.sh # Build and push image to docker hub
#
# The image is tagged with a -firewood-<ref> suffix. Firewood is built by nix
# for the host, so the image is single-arch and the host must be Linux. The
# firewood directory and the go.work replace are left behind.

: "${FIREWOOD_REF:?FIREWOOD_REF must be set to a Firewood branch, tag or commit}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "A Linux host is required: Firewood is built for the host and linked into a Linux image." >&2
  exit 1
fi

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "$AVALANCHE_PATH"

# A previous run leaves a copy that nix can't replace with its symlink.
result=firewood/ffi/result
if [[ -d "$result" && ! -L "$result" ]]; then
  rm -rf "$result"
fi

./scripts/run_polyrepo.sh

# The nix result is a symlink into /nix/store, which Docker won't follow.
cp -rL "$result" "$result.copy"
rm "$result"
mv "$result.copy" "$result"
chmod -R u+w "$result"

IMAGE_TAG_SUFFIX="-firewood-$(printf '%s' "$FIREWOOD_REF" | tr -c 'A-Za-z0-9_.-' '-')"
export IMAGE_TAG_SUFFIX

# Skip building the race image
export SKIP_BUILD_RACE=1

# Only the host's architecture can be built.
export BUILD_MULTI_ARCH=

"$AVALANCHE_PATH"/scripts/build_image.sh "$@"
