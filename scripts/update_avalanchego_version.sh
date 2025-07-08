#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/update_avalanchego_version.sh ]]; then
  echo "must be run from repository root, but got $0"
  exit 255
fi

# If version is not provided, the existing version in go.mod is assumed
VERSION="${1:-}"

if [[ -n "${VERSION}" ]]; then
  echo "Ensuring AvalancheGo version $VERSION in go.mod"
  go get "github.com/ava-labs/avalanchego@${VERSION}"
  go mod tidy
fi

# Discover AVALANCHE_VERSION
. scripts/versions.sh

# The full SHA is required for versioning custom actions.
CURL_ARGS=(curl -s)
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  # Using an auth token avoids being rate limited when run in CI
  CURL_ARGS+=(-H "Authorization: token ${GITHUB_TOKEN}")
fi

GIT_COMMIT=$("${CURL_ARGS[@]}" "https://api.github.com/repos/ava-labs/avalanchego/commits/${AVALANCHE_VERSION}")
FULL_AVALANCHE_VERSION="$(grep -m1 '"sha":' <<< "${GIT_COMMIT}" | cut -d'"' -f4)"

# Ensure the custom action version matches the avalanche version
WORKFLOW_PATH=".github/workflows/ci.yml"
CUSTOM_ACTION="ava-labs/avalanchego/.github/actions/run-monitored-tmpnet-cmd"
echo "Ensuring AvalancheGo version ${FULL_AVALANCHE_VERSION} for ${CUSTOM_ACTION} custom action in ${WORKFLOW_PATH} "
sed -i.bak "s|\(uses: ${CUSTOM_ACTION}\)@.*|\1@${FULL_AVALANCHE_VERSION}|g" "${WORKFLOW_PATH}" && rm -f "${WORKFLOW_PATH}.bak"
