#!/usr/bin/env bash

set -euo pipefail

# Defines an image tag derived from the current branch or tag

image_tag="$( git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true )"
if [[ -z "${image_tag}" ]]; then
  # Supply a default tag when one is not discovered
  image_tag=ci_dummy
elif [[ "${image_tag}" == */* ]]; then
  # Slashes are not legal for docker image tags - replace with dashes
  image_tag="$( echo "${image_tag}" | tr '/' '-' )"
fi
