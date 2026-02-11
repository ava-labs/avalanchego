#!/usr/bin/env bash

# Post-S3-upload validation of RPM packages.
#
# Downloads RPMs and GPG key from S3, then runs a fresh rockylinux:9
# container to verify signature, install, and smoke test. Uses the
# S3-hosted GPG key (not the local build copy) to validate the full
# distribution path end-to-end.
#
# Required env vars:
#   TAG            - Git tag (e.g., "v1.14.1")
#   RPM_ARCH       - RPM architecture ("x86_64" or "aarch64")
#   BUCKET         - S3 bucket name

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${RPM_ARCH:?RPM_ARCH must be set}"
: "${BUCKET:?BUCKET must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

# Source VM ID from constants.sh (canonical definition)
SUBNET_EVM_VM_ID=$(
    grep '^DEFAULT_VM_ID=' "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh" \
    | cut -d'"' -f2
)

WORK_DIR=$(mktemp -d)
trap 'rm -rf "${WORK_DIR}"' EXIT

echo "=== Downloading RPMs and GPG key from S3 ==="
aws s3 cp "s3://${BUCKET}/linux/rpms/${RPM_ARCH}/avalanchego-${TAG}-${RPM_ARCH}.rpm" "${WORK_DIR}/"
aws s3 cp "s3://${BUCKET}/linux/rpms/${RPM_ARCH}/subnet-evm-${TAG}-${RPM_ARCH}.rpm" "${WORK_DIR}/"
aws s3 cp "s3://${BUCKET}/linux/rpms/RPM-GPG-KEY-avalanchego" "${WORK_DIR}/"

echo "=== Validating RPMs in fresh Rocky Linux 9 container ==="
docker run --rm \
    -v "${WORK_DIR}:/rpms:ro" \
    rockylinux:9 \
    bash -euxc '
        # Import GPG key downloaded from S3
        rpm --import /rpms/RPM-GPG-KEY-avalanchego

        # Verify signatures
        rpm -K "/rpms/avalanchego-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"
        rpm -K "/rpms/subnet-evm-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"

        # Install both packages
        rpm -ivh "/rpms/avalanchego-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"
        rpm -ivh "/rpms/subnet-evm-'"${TAG}"'-'"${RPM_ARCH}"'.rpm"

        # Smoke test avalanchego
        output=$(/var/opt/avalanchego/bin/avalanchego --version)
        echo "avalanchego --version: ${output}"
        if [[ "${output}" != avalanchego/* ]]; then
            echo "ERROR: --version output does not start with avalanchego/" >&2
            exit 1
        fi

        # Verify subnet-evm plugin
        plugin="/var/opt/avalanchego/plugins/'"${SUBNET_EVM_VM_ID}"'"
        if [[ ! -x "${plugin}" ]]; then
            echo "ERROR: subnet-evm plugin not found or not executable" >&2
            exit 1
        fi

        echo "All S3 RPM validations passed"
    '

echo "=== S3 RPM validation complete ==="
