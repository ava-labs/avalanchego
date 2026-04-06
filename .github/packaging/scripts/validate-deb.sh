#!/usr/bin/env bash

# Post-build validation of DEB packages.
#
# Validates locally-built DEBs by running fresh Ubuntu containers:
# - ubuntu:22.04 (jammy): signature verification, install, and smoke test
# - ubuntu:24.04 (noble): install and smoke test only (dpkg-sig unavailable)
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
        x86_64)        DEB_ARCH="amd64" ;;
        aarch64|arm64) DEB_ARCH="arm64" ;;
        *)             DEB_ARCH="${arch}" ;;
    esac
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEB_DIR="${REPO_ROOT}/build/deb"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

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

# ── Signature verification (jammy only) ──────────────────────────
# dpkg-sig was removed from Ubuntu 24.04 (noble) repositories.
# Verify signatures in jammy where dpkg-sig is available; the signature
# is embedded in the .deb and does not change between Ubuntu releases.

echo "=== Verifying DEB signatures in fresh ubuntu:22.04 container ==="
docker run --rm \
    -v "${DEB_DIR}:/debs:ro" \
    ubuntu:22.04 \
    bash -euxc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y dpkg-sig gnupg

        if [[ -f /debs/DEB-GPG-KEY-avalanchego ]]; then
            gpg --batch --import /debs/DEB-GPG-KEY-avalanchego
            dpkg-sig --verify "/debs/avalanchego-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
            dpkg-sig --verify "/debs/subnet-evm-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
        else
            echo "Skipping GPG verification (unsigned build)"
        fi
    '

# ── Install and smoke test (both jammy and noble) ────────────────
# Validates that the jammy-built binary installs and runs on both releases.

for UBUNTU_IMAGE in ubuntu:22.04 ubuntu:24.04; do
    echo "=== Install and smoke test in fresh ${UBUNTU_IMAGE} container ==="
    docker run --rm \
        -v "${DEB_DIR}:/debs:ro" \
        -v "${SCRIPTS_DIR}/smoke-test.sh:/smoke-test.sh:ro" \
        "${UBUNTU_IMAGE}" \
        bash -euxc '
            export DEBIAN_FRONTEND=noninteractive

            # Install both packages
            dpkg -i "/debs/avalanchego-'"${TAG}"'-'"${DEB_ARCH}"'.deb"
            dpkg -i "/debs/subnet-evm-'"${TAG}"'-'"${DEB_ARCH}"'.deb"

            # Run shared smoke test
            bash /smoke-test.sh \
                /usr/local/bin/avalanchego \
                /usr/local/lib/avalanchego/plugins \
                "'"${GIT_COMMIT}"'" \
                "'"${SUBNET_EVM_VM_ID}"'"
        '
done

echo "=== DEB validation complete (jammy + noble) ==="
