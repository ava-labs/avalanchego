#!/usr/bin/env bash

# Build the macOS-zip validator Docker image with Go and rcodesign checksum
# verification.
#
# Fetches the SHA256 checksum for the Go tarball from go.dev release metadata
# and for rcodesign from its release sidecar file, then passes both to the
# Docker build for integrity verification.
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

# Map host arch to Go's and rcodesign's naming conventions
arch=$(uname -m)
case "${arch}" in
    x86_64)        goarch="amd64"; rcodesign_arch="x86_64-unknown-linux-musl" ;;
    aarch64|arm64) goarch="arm64"; rcodesign_arch="aarch64-unknown-linux-musl" ;;
    *) echo "Unsupported arch: ${arch}" >&2; exit 1 ;;
esac

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

# ── rcodesign checksum ───────────────────────────────────────────────

rcodesign_filename="apple-codesign-${APPLE_CODESIGN_VERSION}-${rcodesign_arch}.tar.gz"
# The tag uses a slash separator (apple-codesign/<ver>) that must be URL-
# encoded as %2F in the download path.
rcodesign_sha_url="https://github.com/indygreg/apple-platform-rs/releases/download/apple-codesign%2F${APPLE_CODESIGN_VERSION}/${rcodesign_filename}.sha256"
echo "Fetching SHA256 checksum for ${rcodesign_filename}..."
rcodesign_checksum=$(curl -fsSL "${rcodesign_sha_url}" | awk '{print $1}')

if [[ -z "${rcodesign_checksum}" ]]; then
    echo "ERROR: Could not fetch checksum for ${rcodesign_filename} from ${rcodesign_sha_url}" >&2
    exit 1
fi
echo "rcodesign checksum: ${rcodesign_checksum}"

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
