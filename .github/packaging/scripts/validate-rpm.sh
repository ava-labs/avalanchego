#!/usr/bin/env bash

# Post-build validation of RPM packages.
#
# Validates locally-built RPMs by running a fresh rockylinux:9
# container to verify signature, install, and smoke test.
#
# Required env vars:
#   TAG            - Git tag (e.g., "v1.14.1")
#   GIT_COMMIT     - Full git commit hash used to build the binaries
#
# Optional env vars:
#   PACKAGE_ARCH   - RPM architecture ("x86_64" or "aarch64"), defaults to host

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RPM_DIR="${REPO_ROOT}/build/rpm"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-build-common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-validate-common.sh"

resolve_subnet_evm_vm_id
detect_host_arch RPM

assert_files_exist "${RPM_DIR}" \
    "avalanchego-${TAG}-${PACKAGE_ARCH}.rpm" \
    "subnet-evm-${TAG}-${PACKAGE_ARCH}.rpm"

echo "=== Validating RPMs in fresh Rocky Linux 9 container ==="
docker run --rm \
    -v "${RPM_DIR}:/rpms:ro" \
    -v "${SCRIPTS_DIR}/smoke-test.sh:/smoke-test.sh:ro" \
    rockylinux:9 \
    bash -euxc '
        # Import GPG key and verify signatures if available
        if [[ -f /rpms/RPM-GPG-KEY-avalanchego ]]; then
            rpm --import /rpms/RPM-GPG-KEY-avalanchego
            rpm -K "/rpms/avalanchego-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"
            rpm -K "/rpms/subnet-evm-'"${TAG}"'-'"${PACKAGE_ARCH}"'.rpm"
        else
            echo "Skipping GPG verification (unsigned build)"
        fi

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
