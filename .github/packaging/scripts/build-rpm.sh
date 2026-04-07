#!/usr/bin/env bash

# Build and sign an RPM package inside the container.
#
# Required env vars:
#   PACKAGE        - "avalanchego" or "subnet-evm"
#   VERSION        - Semantic version without "v" prefix (e.g., "1.14.1")
#   TAG            - Git tag (e.g., "v1.14.1")
#   PACKAGE_ARCH       - RPM architecture name ("x86_64" or "aarch64")
#   OUTPUT_DIR     - Directory for the output RPM (bind-mounted from host)
#
# Optional env vars:
#   RPM_GPG_KEY_FILE      - Path to GPG private key for signing
#   NFPM_RPM_PASSPHRASE   - Passphrase for the GPG key
#   AVALANCHEGO_COMMIT    - Git commit hash (auto-detected if not set)

set -euo pipefail

: "${PACKAGE:?PACKAGE must be set (avalanchego or subnet-evm)}"
: "${VERSION:?VERSION must be set}"
: "${TAG:?TAG must be set}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"

# shellcheck disable=SC1091
source "${PACKAGING_DIR}/scripts/lib-build-common.sh"

# Well-known paths referenced by nfpm configs
export NFPM_CHANGELOG="${REPO_ROOT}/build/nfpm-changelog.yml"
export NFPM_SIGNING_KEY="${REPO_ROOT}/build/gpg/signing-key.asc"

echo "=== Building ${PACKAGE} RPM for ${PACKAGE_ARCH} (tag: ${TAG}) ==="

init_build_env
build_binary "${PACKAGE}"
generate_changelog "${VERSION}"

# ── GPG signing ───────────────────────────────────────────────────

GPG_PUBLIC_KEY="${OUTPUT_DIR}/RPM-GPG-KEY-avalanchego"
setup_gpg "${RPM_GPG_KEY_FILE:-}" "${GPG_PUBLIC_KEY}" "RPM"

# Ephemeral keys have no passphrase; nfpm needs the variable set to empty.
# The workflow always exports NFPM_RPM_PASSPHRASE (from secrets) but skips
# RPM_GPG_KEY_FILE on PR builds, so we must clear it unconditionally here.
if [[ -z "${RPM_GPG_KEY_FILE:-}" ]]; then
    export NFPM_RPM_PASSPHRASE=""
fi

# ── Package with nfpm ─────────────────────────────────────────────

case "${PACKAGE}" in
    avalanchego) export AVALANCHEGO_BINARY="${BINARY_PATH}" ;;
    subnet-evm)  export SUBNET_EVM_BINARY="${BINARY_PATH}" ;;
esac

export VERSION PACKAGE_ARCH

RPM_FILENAME="${PACKAGE}-${TAG}-${PACKAGE_ARCH}.rpm"
RPM_PATH="${OUTPUT_DIR}/${RPM_FILENAME}"

run_nfpm_package \
    "${PACKAGING_DIR}/nfpm/${PACKAGE}-rpm.yml" \
    "${REPO_ROOT}/build/${PACKAGE}-rpm-resolved.yml" \
    rpm \
    "${RPM_PATH}"

echo "RPM built: ${RPM_PATH}"
