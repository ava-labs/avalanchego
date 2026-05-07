#!/usr/bin/env bash

# Build a packaging builder Docker image with Go checksum verification.
#
# Fetches the SHA256 checksum for the Go tarball from go.dev release
# metadata and passes it to the Docker build for integrity verification.
#
# Required env vars:
#   GO_VERSION    - Go version to install (e.g., "1.24.12")
#   DOCKER_IMAGE  - Name for the built Docker image
#   CONTEXT_DIR   - Path to the Dockerfile directory
#
# Optional env vars:
#   DOCKERFILE    - Dockerfile name within CONTEXT_DIR (default: "Dockerfile")

set -euo pipefail

: "${GO_VERSION:?GO_VERSION must be set}"
: "${DOCKER_IMAGE:?DOCKER_IMAGE must be set}"
: "${CONTEXT_DIR:?CONTEXT_DIR must be set}"

DOCKERFILE="${DOCKERFILE:-Dockerfile}"

command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not found on PATH" >&2; exit 1; }

# Map host arch to Go's naming convention
arch=$(uname -m)
case "${arch}" in
    x86_64)        goarch="amd64" ;;
    aarch64|arm64) goarch="arm64" ;;
    *) echo "Unsupported arch: ${arch}" >&2; exit 1 ;;
esac

# Fetch SHA256 checksum from go.dev release metadata
filename="go${GO_VERSION}.linux-${goarch}.tar.gz"
echo "Fetching SHA256 checksum for ${filename}..."
checksum=$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" \
    | jq -r --arg fn "${filename}" \
          '[.[] | .files[] | select(.filename == $fn) | .sha256] | first')

if [[ -z "${checksum}" || "${checksum}" == "null" ]]; then
    echo "ERROR: Could not find checksum for ${filename}" >&2
    exit 1
fi
echo "Go checksum: ${checksum}"

# The docker-container driver leaves the result in BuildKit cache unless we
# explicitly load it into the local image store for the subsequent docker run.
build_flags=()
build_driver=$(
    docker buildx inspect 2>/dev/null \
        | awk '/^Driver:/ { print $2; exit }'
)
if [[ "${build_driver}" == "docker-container" ]]; then
    build_flags+=(--load)
fi

# Pin the build to the host arch we resolved goarch / GO_CHECKSUM for.
# Without this, DOCKER_DEFAULT_PLATFORM (commonly set on Apple Silicon)
# could make Docker's TARGETARCH diverge from the checksum we computed,
# causing the Dockerfile's Go SHA256 verification to fail.
build_flags+=(--platform "linux/${goarch}")

docker build "${build_flags[@]}" \
    --build-arg GO_VERSION="${GO_VERSION}" \
    --build-arg GO_CHECKSUM="${checksum}" \
    -f "${CONTEXT_DIR}/${DOCKERFILE}" \
    -t "${DOCKER_IMAGE}" \
    "${CONTEXT_DIR}"
