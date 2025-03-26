#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

if [[ -z ${AVALANCHE_VERSION:-} ]]; then
  # Get module details from go.mod
  MODULE_DETAILS="$(go list -m "github.com/ava-labs/avalanchego" 2>/dev/null)"

  # Extract the version part
  AVALANCHE_VERSION="$(echo "${MODULE_DETAILS}" | awk '{print $2}')"

  # Check if the version matches the pattern where the last part is the module hash
  # v*YYYYMMDDHHMMSS-abcdef123456
  #
  # If not, the value is assumed to represent a tag
  if [[ "${AVALANCHE_VERSION}" =~ ^v.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
    # Extract module hash from version
    MODULE_HASH="$(echo "${AVALANCHE_VERSION}" | grep -Eo '[0-9a-f]{12}$')"

    # The first 8 chars of the hash is used as the tag of avalanchego images
    AVALANCHE_VERSION="${MODULE_HASH::8}"
  fi
fi
