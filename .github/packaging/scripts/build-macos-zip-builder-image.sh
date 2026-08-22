#!/usr/bin/env bash

# Build the macOS-zip validator Docker image with Go and rcodesign checksum
# verification.
#
# Fetches the SHA256 checksum for the Go tarball from go.dev release metadata,
# uses a repo-pinned digest for rcodesign, then passes both to the Docker build
# for integrity verification.
#
# Required env vars:
#   GO_VERSION              - Go version to install (e.g., "1.24.12")
#   APPLE_CODESIGN_VERSION  - rcodesign version (e.g., "0.28.0")
#   DOCKER_IMAGE            - Name for the built Docker image
#   CONTEXT_DIR             - Path to the Dockerfile directory
#   DOCKERFILE              - Dockerfile name (e.g., "Dockerfile.macos-zip")

set -euo pipefail

: "${GO_VERSION:?GO_VERSION must be set}"
: "${APPLE_CODESIGN_VERSION:?APPLE_CODESIGN_VERSION must be set}"
: "${DOCKER_IMAGE:?DOCKER_IMAGE must be set}"
: "${CONTEXT_DIR:?CONTEXT_DIR must be set}"
: "${DOCKERFILE:?DOCKERFILE must be set (e.g. Dockerfile.macos-zip)}"

command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not found on PATH" >&2; exit 1; }

# Map host arch to Go's naming convention, and pin the rcodesign release-tarball
# SHA256 per arch. The digests are pinned in this repo
# (rather than fetched from the release's own .sha256 sidecar) so a retagged or
# replaced upstream release cannot swap the signing binary. Keep in sync with
# APPLE_CODESIGN_SHA256 / APPLE_CODESIGN_PKG_VERSION in
# .github/workflows/build-macos-release.yml. Digests are version-specific.
readonly PINNED_APPLE_CODESIGN_VERSION="0.28.0"
arch=$(uname -m)
case "${arch}" in
    x86_64)        goarch="amd64"; rcodesign_checksum="d0f0e4c7915295a1e79fc8b775b354c76d621a52da4c6b841dc9f0585de06cca" ;;
    aarch64|arm64) goarch="arm64"; rcodesign_checksum="1d3550680c0444b24b1387753f46efc7dcdb336f9001dbde4508f1c58da90b81" ;;
    *) echo "Unsupported arch: ${arch}" >&2; exit 1 ;;
esac

# The pinned digests above are only valid for one rcodesign version. Fail loudly
# if the requested version has drifted, rather than verifying against the wrong
# digest.
if [[ "${APPLE_CODESIGN_VERSION}" != "${PINNED_APPLE_CODESIGN_VERSION}" ]]; then
    echo "ERROR: no pinned rcodesign digest for version ${APPLE_CODESIGN_VERSION} (pinned: ${PINNED_APPLE_CODESIGN_VERSION}); update the digests in $(basename "$0")" >&2
    exit 1
fi

# ── Go checksum ──────────────────────────────────────────────────────

go_filename="go${GO_VERSION}.linux-${goarch}.tar.gz"
echo "Fetching SHA256 checksum for ${go_filename}..."
go_checksum=$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" \
    | jq -r --arg fn "${go_filename}" \
          '[.[] | .files[] | select(.filename == $fn) | .sha256] | first')

if [[ -z "${go_checksum}" || "${go_checksum}" == "null" ]]; then
    echo "ERROR: Could not find checksum for ${go_filename}" >&2
    exit 1
fi
echo "Go checksum: ${go_checksum}"
echo "rcodesign checksum (pinned): ${rcodesign_checksum}"

# ── Build ───────────────────────────────────────────────────────────

# The docker-container driver leaves the result in BuildKit cache unless we
# explicitly load it into the local image store for the subsequent docker run.
build_flags=()
build_driver=$(
    docker buildx inspect 2>/dev/null \
        | awk '/^Driver:/ { print $2; exit }'
) || true
if [[ "${build_driver}" == "docker-container" ]]; then
    build_flags+=(--load)
fi

docker build "${build_flags[@]}" \
    --build-arg GO_VERSION="${GO_VERSION}" \
    --build-arg GO_CHECKSUM="${go_checksum}" \
    --build-arg APPLE_CODESIGN_VERSION="${APPLE_CODESIGN_VERSION}" \
    --build-arg APPLE_CODESIGN_CHECKSUM="${rcodesign_checksum}" \
    -f "${CONTEXT_DIR}/${DOCKERFILE}" \
    -t "${DOCKER_IMAGE}" \
    "${CONTEXT_DIR}"
