#!/usr/bin/env bash

# Post-build validation of RPM packages.
#
# Validates locally-built RPMs by running a fresh rockylinux:9
# container to verify signature, install, and smoke test.

set -euo pipefail

: "${TAG:?TAG must be set (Git tag, e.g. v1.14.1)}"
: "${GIT_COMMIT:?GIT_COMMIT must be set (full git commit hash used to build the binaries)}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set (x86_64 or aarch64)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RPM_DIR="${REPO_ROOT}/build/rpm"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-build-common.sh"

resolve_subnet_evm_vm_id

assert_files_exist "${RPM_DIR}" \
    "avalanchego-${TAG}-${PACKAGE_ARCH}.rpm" \
    "subnet-evm-${TAG}-${PACKAGE_ARCH}.rpm"

echo "=== Validating RPMs in fresh Rocky Linux 9 container ==="
docker run --rm \
    -v "${RPM_DIR}:/rpms:ro" \
    -v "${SCRIPTS_DIR}/smoke-test.sh:/smoke-test.sh:ro" \
    rockylinux:9 \
    bash -euxc '
        # Import GPG key and verify signatures (always produced by the build).
        if [[ ! -f /rpms/GPG-KEY-avalanchego ]]; then
            echo "ERROR: GPG-KEY-avalanchego not found; build did not export a key" >&2
            exit 1
        fi
        rpm --import /rpms/GPG-KEY-avalanchego
        rpm -K "/rpms/avalanchego-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"
        rpm -K "/rpms/subnet-evm-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"

        # Install both packages
        rpm -ivh "/rpms/avalanchego-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"
        rpm -ivh "/rpms/subnet-evm-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"

        # Run shared smoke test
        bash /smoke-test.sh \
            /var/opt/avalanchego/bin/avalanchego \
            /var/opt/avalanchego/plugins \
            "'"${GIT_COMMIT}"'" \
            "'"${SUBNET_EVM_VM_ID}"'"
    '

echo "=== RPM validation complete ==="
