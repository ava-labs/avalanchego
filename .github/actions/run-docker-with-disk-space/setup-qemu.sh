#!/usr/bin/env bash

set -euo pipefail

# This implements the QEMU installation performed by docker/setup-qemu-action
# without its cache behavior. It is derived from:
# https://github.com/docker/setup-qemu-action/blob/ce360397dd3f832beb865e1373c09c0e9f86d70a/src/main.ts
#
# BINFMT_IMAGE must match the Docker host architecture. Current callers and
# the default image use Linux AMD64. See docs/ci.md#binfmt-image-cache.
#
# Docker save/load does not retain digest references, so save and run a local
# tag derived from the pinned digest.
# On a miss, pull by digest before applying the tag. Master runs authenticate;
# PRs pull anonymously. Only permitted master runs save the image for PRs.
cache_file="$HOME/.cache/avalanchego/binfmt/image.tar"
local_image="avalanchego-binfmt:${BINFMT_IMAGE##*@sha256:}"
if [[ "$BINFMT_CACHE_HIT" == "true" ]]; then
  docker load --input "$cache_file"
else
  docker pull "$BINFMT_IMAGE"
  docker tag "$BINFMT_IMAGE" "$local_image"
  if [[ "$CACHE_BINFMT_IMAGE" == "true" ]]; then
    mkdir -p "${cache_file%/*}"
    docker image save --output "$cache_file" "$local_image"
  fi
fi
docker run --privileged --rm "$local_image" --install all
