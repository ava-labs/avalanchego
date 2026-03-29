#!/usr/bin/env bash

# Post-build validation of DEB packages.
#
# Validates locally-built DEBs by running a fresh ubuntu:22.04
# container to verify signature, install, and smoke test.
#
# Required env vars:
#   TAG            - Git tag (e.g., "v1.14.1")
#   GIT_COMMIT     - Full git commit hash used to build the binaries
#
# Optional env vars:
#   DEB_ARCH       - DEB architecture ("amd64" or "arm64"), defaults to host

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

if [[ -z "${DEB_ARCH:-}" ]]; then
    arch=$(uname -m)
    case "${arch}" in
        x86_64)       DEB_ARCH="amd64" ;;
        aarch64|arm64) DEB_ARCH="arm64" ;;
        *)            DEB_ARCH="${arch}" ;;
    esac
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEB_DIR="${REPO_ROOT}/build/deb"

# Source VM ID from constants.sh (canonical definition)
SUBNET_EVM_VM_ID=$(
    grep '^DEFAULT_VM_ID=' "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh" \
    | cut -d'"' -f2
)

# Verify expected files exist
for f in \
    "avalanchego-${TAG}-${DEB_ARCH}.deb" \
    "subnet-evm-${TAG}-${DEB_ARCH}.deb" \
; do
    if [[ ! -f "${DEB_DIR}/${f}" ]]; then
        echo "ERROR: expected file not found: ${DEB_DIR}/${f}" >&2
        exit 1
    fi
done

echo "=== Validating DEBs in fresh Ubuntu 22.04 container ==="
docker run --rm \
    -v "${DEB_DIR}:/debs:ro" \
    ubuntu:22.04 \
    bash -euxc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y dpkg-sig gnupg

        # Import GPG key and verify signatures if available
        if [[ -f /debs/DEB-GPG-KEY-avalanchego ]]; then
            gpg --batch --import /debs/DEB-GPG-KEY-avalanchego
            dpkg-sig --verify "/debs/avalanchego-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
            dpkg-sig --verify "/debs/subnet-evm-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
        else
            echo "Skipping GPG verification (unsigned build)"
        fi

        # Install both packages
        dpkg -i "/debs/avalanchego-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
        dpkg -i "/debs/subnet-evm-'"${TAG}"'-'"${DEB_ARCH}"'.deb"

        # Smoke test avalanchego
        full_commit="'"${GIT_COMMIT}"'"
        output=$(/usr/local/bin/avalanchego --version)
        echo "avalanchego --version: ${output}"
        if [[ "${output}" != avalanchego/* ]]; then
            echo "ERROR: --version output does not start with avalanchego/" >&2
            exit 1
        fi
        if [[ "${output}" != *"${full_commit}"* ]]; then
            echo "ERROR: avalanchego --version output does not contain expected commit ${full_commit}" >&2
            echo "Output: ${output}" >&2
            exit 1
        fi

        # Verify subnet-evm plugin
        plugin="/usr/local/lib/avalanchego/plugins/'"${SUBNET_EVM_VM_ID}"'"
        if [[ ! -x "${plugin}" ]]; then
            echo "ERROR: subnet-evm plugin not found or not executable" >&2
            exit 1
        fi

        # Smoke test subnet-evm version and commit
        evm_output=$("${plugin}" --version)
        echo "subnet-evm --version: ${evm_output}"
        if [[ "${evm_output}" != *"${full_commit}"* ]]; then
            echo "ERROR: subnet-evm --version output does not contain expected commit ${full_commit}" >&2
            echo "Output: ${evm_output}" >&2
            exit 1
        fi

        echo "All DEB validations passed"
    '

echo "=== DEB validation complete ==="
