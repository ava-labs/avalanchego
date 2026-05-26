#!/usr/bin/env bash

# Post-build validation of DEB packages.
#
# Validates locally-built DEBs by running fresh Ubuntu containers for both
# jammy (22.04) and noble (24.04): verify the nFPM-native _gpgorigin
# signature, install the package, and run the smoke test.

set -euo pipefail

: "${TAG:?TAG must be set (Git tag, e.g. v1.14.1)}"
: "${GIT_COMMIT:?GIT_COMMIT must be set (full git commit hash used to build the binaries)}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set (amd64 or arm64)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEB_DIR="${REPO_ROOT}/build/deb"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-build-common.sh"

resolve_subnet_evm_vm_id

assert_files_exist "${DEB_DIR}" \
    "avalanchego-${TAG}-${PACKAGE_ARCH}.deb" \
    "subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

# ── Verify + install + smoke test (both jammy and noble) ─────────
# nfpm stores a detached GPG signature in the `_gpgorigin` ar member,
# covering debian-binary + control.tar.* + data.tar.* concatenated in
# ar-member order. Verifying with `gpg --verify` keeps the same flow
# on every supported Ubuntu release.

for UBUNTU_IMAGE in ubuntu:22.04 ubuntu:24.04; do
    echo "=== Verify, install and smoke test in fresh ${UBUNTU_IMAGE} container ==="
    docker run --rm \
        -v "${DEB_DIR}:/debs:ro" \
        -v "${SCRIPTS_DIR}/smoke-test.sh:/smoke-test.sh:ro" \
        -e "TAG=${TAG}" \
        -e "PACKAGE_ARCH=${PACKAGE_ARCH}" \
        -e "GIT_COMMIT=${GIT_COMMIT}" \
        -e "SUBNET_EVM_VM_ID=${SUBNET_EVM_VM_ID}" \
        "${UBUNTU_IMAGE}" \
        bash -euxc '
            export DEBIAN_FRONTEND=noninteractive
            apt-get update
            apt-get install -y binutils gnupg

            verify_deb_signature() {
                local deb="$1"
                local workdir
                workdir=$(mktemp -d)
                ( cd "${workdir}" && ar x "${deb}" )
                cat "${workdir}/debian-binary" \
                    "${workdir}"/control.tar.* \
                    "${workdir}"/data.tar.* > "${workdir}/combined"
                gpg --verify "${workdir}/_gpgorigin" "${workdir}/combined"
                rm -rf "${workdir}"
            }

            # Import GPG key and verify signatures (always produced by the build).
            if [[ ! -f /debs/DEB-GPG-KEY-avalanchego ]]; then
                echo "ERROR: DEB-GPG-KEY-avalanchego not found; build did not export a key" >&2
                exit 1
            fi
            gpg --batch --import /debs/DEB-GPG-KEY-avalanchego
            verify_deb_signature "/debs/avalanchego-${TAG}-${PACKAGE_ARCH}.deb"
            verify_deb_signature "/debs/subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

            dpkg -i "/debs/avalanchego-${TAG}-${PACKAGE_ARCH}.deb"
            dpkg -i "/debs/subnet-evm-${TAG}-${PACKAGE_ARCH}.deb"

            bash /smoke-test.sh \
                /usr/local/bin/avalanchego \
                "/usr/local/lib/avalanchego/plugins/${SUBNET_EVM_VM_ID}" \
                "${GIT_COMMIT}"
        '
done

echo "=== DEB validation complete (jammy + noble) ==="
