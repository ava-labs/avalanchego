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
#   PACKAGE_ARCH   - DEB architecture ("amd64" or "arm64"), defaults to host

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEB_DIR="${REPO_ROOT}/build/deb"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-build-common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-validate-common.sh"

detect_host_arch DEB
resolve_subnet_evm_vm_id

assert_files_exist "${DEB_DIR}" \
    "avalanchego-${TAG}-${PACKAGE_ARCH}.deb" \
    "subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

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
            dpkg-sig --verify "/debs/avalanchego-'"${TAG}"'-'"${PACKAGE_ARCH}"'.deb"
            dpkg-sig --verify "/debs/subnet-evm-'"${TAG}"'-'"${PACKAGE_ARCH}"'.deb"
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
            dpkg -i "/debs/avalanchego-'"${TAG}"'-'"${PACKAGE_ARCH}"'.deb"
            dpkg -i "/debs/subnet-evm-'"${TAG}"'-'"${PACKAGE_ARCH}"'.deb"

            # Run shared smoke test
            bash /smoke-test.sh \
                /usr/local/bin/avalanchego \
                /usr/local/lib/avalanchego/plugins \
                "'"${GIT_COMMIT}"'" \
                "'"${SUBNET_EVM_VM_ID}"'"
        '
done

echo "=== DEB validation complete (jammy + noble) ==="
