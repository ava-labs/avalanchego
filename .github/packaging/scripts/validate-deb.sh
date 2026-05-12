#!/usr/bin/env bash

# Post-build validation of DEB packages.
#
# Validates locally-built DEBs by running fresh Ubuntu containers
# (both 22.04/jammy and 24.04/noble) to verify signature, install, and
# smoke test the installed binaries.
#
# Signature verification uses `ar x` + `gpg --verify` (see
# verify-deb-signature.sh), which is tool-independent and works
# identically across jammy and noble.
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

resolve_subnet_evm_vm_id
detect_host_arch DEB

assert_files_exist "${DEB_DIR}" \
    "avalanchego-${TAG}-${PACKAGE_ARCH}.deb" \
    "subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

for ubuntu_tag in 22.04 24.04; do
    echo "=== Validating DEBs in fresh ubuntu:${ubuntu_tag} container ==="
    docker run --rm \
        -v "${DEB_DIR}:/debs:ro" \
        -v "${SCRIPTS_DIR}/smoke-test.sh:/smoke-test.sh:ro" \
        -v "${SCRIPTS_DIR}/verify-deb-signature.sh:/verify-deb-signature.sh:ro" \
        -e TAG="${TAG}" \
        -e PACKAGE_ARCH="${PACKAGE_ARCH}" \
        -e GIT_COMMIT="${GIT_COMMIT}" \
        -e SUBNET_EVM_VM_ID="${SUBNET_EVM_VM_ID}" \
        "ubuntu:${ubuntu_tag}" \
        bash -euxc '
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq
            apt-get install -y --no-install-recommends gnupg binutils ca-certificates >/dev/null

            if [[ -f /debs/DEB-GPG-KEY-avalanchego ]]; then
                gpg --batch --import /debs/DEB-GPG-KEY-avalanchego
                bash /verify-deb-signature.sh "/debs/avalanchego-${TAG}-${PACKAGE_ARCH}.deb"
                bash /verify-deb-signature.sh "/debs/subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"
            else
                echo "Skipping GPG verification (unsigned build)"
            fi

            dpkg -i "/debs/avalanchego-${TAG}-${PACKAGE_ARCH}.deb"
            dpkg -i "/debs/subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

            bash /smoke-test.sh \
                /usr/local/bin/avalanchego \
                /usr/local/lib/avalanchego/plugins \
                "${GIT_COMMIT}" \
                "${SUBNET_EVM_VM_ID}"
        '
done

echo "=== DEB validation complete (jammy + noble) ==="
