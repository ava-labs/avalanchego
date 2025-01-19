#!/usr/bin/env bash

set -euo pipefail

# Emits an image tag derived from the current branch or tag

IMAGE_TAG="$( git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true )"
if [[ -z "${IMAGE_TAG}" ]]; then
  # Supply a default tag when one is not discovered
  IMAGE_TAG=ci_dummy
elif [[ "${IMAGE_TAG}" == */* ]]; then
  # Slashes are not legal for docker image tags - replace with dashes
  IMAGE_TAG="$( echo "${IMAGE_TAG}" | tr '/' '-' )"
fi
echo "${IMAGE_TAG}"
