#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Upstream is compatible with v1.54.x at time of this writing, and
# checking for this specific version is an attempt to avoid skew
# between local and CI execution. The latest version (v1.55.1) seems
# to cause spurious failures
KNOWN_GOOD_VERSION="v1.54"
VERSION="$(golangci-lint --version | sed -e 's+golangci-lint has version \(v1.*\)\..* built.*+\1+')"
if [[ "${VERSION}" != "${KNOWN_GOOD_VERSION}" ]]; then
  echo "expected golangci-lint ${KNOWN_GOOD_VERSION}, but ${VERSION} was used"
  echo "${KNOWN_GOOD_VERSION} is used in CI and should be used locally to ensure compatible results"
  echo "installation command: go install github.com/golangci/golangci-lint/cmd/golangci-lint@${KNOWN_GOOD_VERSION}"
  exit 255
fi

golangci-lint run --path-prefix=. --timeout 3m
