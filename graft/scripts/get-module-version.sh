#!/usr/bin/env bash

set -euo pipefail

# Extracts the version for a Go module from go.mod
# This script outputs ONLY the version string to stdout for use in shell command substitution.
#
# Usage: get-module-version.sh <module-path>
# Example: get-module-version.sh github.com/ava-labs/coreth
#
# Output format:
#   - If module uses a pseudo-version (vX.Y.Z-YYYYMMDDHHMMSS-abcdef123456):
#     Outputs the first 8 characters of the 12-char commit hash
#   - If module uses a regular tag/version:
#     Outputs the version as-is

if [ $# -ne 1 ]; then
  echo "Error: exactly one argument required" >&2
  echo "Usage: $0 <module-path>" >&2
  echo "Example: $0 github.com/ava-labs/coreth" >&2
  exit 1
fi

MODULE_PATH="$1"

# Get module details from go.mod
MODULE_DETAILS="$(go list -m "${MODULE_PATH}" 2>/dev/null || true)"
if [ -z "${MODULE_DETAILS}" ]; then
  echo "Error: module ${MODULE_PATH} not found in go.mod" >&2
  exit 1
fi

# Extract version from module details (second field)
VERSION="$(echo "${MODULE_DETAILS}" | awk '{print $2}')"

if [ -z "${VERSION}" ]; then
  echo "Error: could not extract version from module details: ${MODULE_DETAILS}" >&2
  exit 1
fi

# Check if version is a module hash (pseudo-version format: v*YYYYMMDDHHMMSS-abcdef123456)
if [[ "${VERSION}" =~ ^v.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
  # Extract the 12-character commit hash from the end
  MODULE_HASH="$(echo "${VERSION}" | grep -Eo '[0-9a-f]{12}$')"
  # Use the first 8 characters of the hash
  # This is the convention used for avalanchego image tags
  echo "${MODULE_HASH::8}"
else
  # Regular tag/version - output as-is
  echo "${VERSION}"
fi
