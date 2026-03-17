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
#   RPM_ARCH       - RPM architecture ("x86_64" or "aarch64"), defaults to host

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

if [[ -z "${RPM_ARCH:-}" ]]; then
    arch=$(uname -m)
    case "${arch}" in
        x86_64) RPM_ARCH="x86_64" ;;
        arm64)  RPM_ARCH="aarch64" ;;
        *)      RPM_ARCH="${arch}" ;;
    esac
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RPM_DIR="${REPO_ROOT}/build/rpm"

# Source VM ID from constants.sh (canonical definition)
SUBNET_EVM_VM_ID=$(
    grep '^DEFAULT_VM_ID=' "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh" \
    | cut -d'"' -f2
)

# Verify expected files exist
for f in \
    "avalanchego-${TAG}-${RPM_ARCH}.rpm" \
    "subnet-evm-${TAG}-${RPM_ARCH}.rpm" \
; do
    if [[ ! -f "${RPM_DIR}/${f}" ]]; then
        echo "ERROR: expected file not found: ${RPM_DIR}/${f}" >&2
        exit 1
    fi
done

echo "=== Validating RPMs in fresh Rocky Linux 9 container ==="
docker run --rm \
    -v "${RPM_DIR}:/rpms:ro" \
    rockylinux:9 \
    bash -euxc '
        # Import GPG key and verify signatures if available
        if [[ -f /rpms/RPM-GPG-KEY-avalanchego ]]; then
            rpm --import /rpms/RPM-GPG-KEY-avalanchego
            rpm -K "/rpms/avalanchego-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"
            rpm -K "/rpms/subnet-evm-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"
        else
            echo "Skipping GPG verification (unsigned build)"
        fi

        # Install both packages
        rpm -ivh "/rpms/avalanchego-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"
        rpm -ivh "/rpms/subnet-evm-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"

        # Smoke test avalanchego
        full_commit="'"${GIT_COMMIT}"'"
        output=$(/var/opt/avalanchego/bin/avalanchego --version)
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
        plugin="/var/opt/avalanchego/plugins/'"${SUBNET_EVM_VM_ID}"'"
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

        echo "All RPM validations passed"
    '

echo "=== RPM validation complete ==="
