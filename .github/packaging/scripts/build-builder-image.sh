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

# Resolve the target arch for the builder image. Default: host arch.
# When DOCKER_DEFAULT_PLATFORM is set to a non-host arch (a common
# Apple Silicon setup), require an explicit ALLOW_CROSS_BUILD=1 opt-in
# rather than silently cross-building under qemu emulation.
case "$(uname -m)" in
    x86_64)        host_goarch="amd64" ;;
    aarch64|arm64) host_goarch="arm64" ;;
    *) echo "Unsupported host arch: $(uname -m)" >&2; exit 1 ;;
esac
goarch="${host_goarch}"
if [[ -n "${DOCKER_DEFAULT_PLATFORM:-}" ]]; then
    case "${DOCKER_DEFAULT_PLATFORM}" in
        linux/amd64) ddp_goarch="amd64" ;;
        linux/arm64) ddp_goarch="arm64" ;;
        *) echo "Unsupported DOCKER_DEFAULT_PLATFORM: ${DOCKER_DEFAULT_PLATFORM}" >&2; exit 1 ;;
    esac
    if [[ "${ddp_goarch}" != "${host_goarch}" ]]; then
        if [[ "${ALLOW_CROSS_BUILD:-}" != "1" ]]; then
            cat >&2 <<MSG
ERROR: DOCKER_DEFAULT_PLATFORM=${DOCKER_DEFAULT_PLATFORM} differs from
       the host arch (linux/${host_goarch}). Cross-building via qemu
       is slow and produces binaries that don't run on the host.

       To proceed anyway, re-run with ALLOW_CROSS_BUILD=1.
MSG
            exit 1
        fi
        echo "ALLOW_CROSS_BUILD=1: cross-building for ${DOCKER_DEFAULT_PLATFORM}."
        goarch="${ddp_goarch}"
    fi
fi

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
) || true
if [[ "${build_driver}" == "docker-container" ]]; then
    build_flags+=(--load)
fi

# Pin the build to the arch we resolved goarch / GO_CHECKSUM for, so the
# Dockerfile's Go SHA256 verification matches Docker's TARGETARCH
# regardless of any DOCKER_DEFAULT_PLATFORM ambient setting.
build_flags+=(--platform "linux/${goarch}")

docker build "${build_flags[@]}" \
    --build-arg GO_VERSION="${GO_VERSION}" \
    --build-arg GO_CHECKSUM="${checksum}" \
    -f "${CONTEXT_DIR}/${DOCKERFILE}" \
    -t "${DOCKER_IMAGE}" \
    "${CONTEXT_DIR}"
