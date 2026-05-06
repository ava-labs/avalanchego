#!/usr/bin/env bash

# Build and sign a package (RPM or DEB) inside the container.
#
# Required env vars:
#   PKG_FORMAT     - Package format: "RPM" or "DEB" (default: RPM)
#   PACKAGE        - "avalanchego" or "subnet-evm"
#   VERSION        - Semantic version without "v" prefix (e.g., "1.14.1")
#   TAG            - Git tag (e.g., "v1.14.1")
#   PACKAGE_ARCH   - Architecture (x86_64/aarch64 for RPM, amd64/arm64 for DEB)
#   OUTPUT_DIR     - Directory for the output package (bind-mounted from host)
#
# Optional env vars (format-prefixed by convention):
#   RPM_GPG_KEY_FILE / DEB_GPG_KEY_FILE        - Path to GPG private key
#   NFPM_RPM_PASSPHRASE / NFPM_DEB_PASSPHRASE  - GPG passphrase
#   AVALANCHEGO_COMMIT                         - Git commit hash (auto-detected if not set)

set -euo pipefail

: "${PACKAGE:?PACKAGE must be set (avalanchego or subnet-evm)}"
: "${VERSION:?VERSION must be set}"
: "${TAG:?TAG must be set}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set}"

PKG_FORMAT="${PKG_FORMAT:-RPM}"
pkg_format_lower="${PKG_FORMAT,,}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"

# shellcheck disable=SC1091
source "${PACKAGING_DIR}/scripts/lib-build-common.sh"

# Source format-specific extensions if available (e.g., lib-build-deb.sh)
fmt_extensions="${PACKAGING_DIR}/scripts/lib-build-${pkg_format_lower}.sh"
if [[ -f "${fmt_extensions}" ]]; then
    # shellcheck disable=SC1090
    source "${fmt_extensions}"
fi

# Well-known paths referenced by nfpm configs
export NFPM_CHANGELOG="${REPO_ROOT}/build/nfpm-changelog.yml"
export NFPM_SIGNING_KEY="${REPO_ROOT}/build/gpg/signing-key.asc"

echo "=== Building ${PACKAGE} ${PKG_FORMAT} for ${PACKAGE_ARCH} (tag: ${TAG}) ==="

init_build_env
build_binary "${PACKAGE}"
generate_changelog "${VERSION}"

# ── GPG signing ───────────────────────────────────────────────────

# Resolve format-prefixed GPG key file variable
case "${PKG_FORMAT}" in
    RPM) GPG_KEY_FILE="${RPM_GPG_KEY_FILE:-}" ;;
    DEB) GPG_KEY_FILE="${DEB_GPG_KEY_FILE:-}" ;;
esac

GPG_PUBLIC_KEY="${OUTPUT_DIR}/${PKG_FORMAT}-GPG-KEY-avalanchego"

# DEB needs gpg-agent configured before GPG setup
if [[ "${PKG_FORMAT}" == "DEB" ]] && type -t setup_deb_gpg_agent &>/dev/null; then
    setup_deb_gpg_agent
fi

setup_gpg "${GPG_KEY_FILE}" "${GPG_PUBLIC_KEY}" "${PKG_FORMAT}"

# Format-specific post-GPG handling
case "${PKG_FORMAT}" in
    RPM)
        # Ephemeral keys have no passphrase; nfpm needs the variable set empty
        if [[ -z "${GPG_KEY_FILE}" ]]; then
            export NFPM_RPM_PASSPHRASE=""
        fi
        ;;
    DEB)
        # Cache passphrase in gpg-agent for dpkg-sig
        if type -t cache_deb_gpg_passphrase &>/dev/null; then
            cache_deb_gpg_passphrase "${GPG_KEY_FILE}"
        fi
        ;;
esac

# ── Package with nfpm ─────────────────────────────────────────────

case "${PACKAGE}" in
    avalanchego) export AVALANCHEGO_BINARY="${BINARY_PATH}" ;;
    subnet-evm)  export SUBNET_EVM_BINARY="${BINARY_PATH}" ;;
esac

export VERSION PACKAGE_ARCH

PKG_FILENAME="${PACKAGE}-${TAG}-${PACKAGE_ARCH}.${pkg_format_lower}"
PKG_PATH="${OUTPUT_DIR}/${PKG_FILENAME}"

run_nfpm_package \
    "${PACKAGING_DIR}/nfpm/${PACKAGE}-${pkg_format_lower}.yml" \
    "${REPO_ROOT}/build/${PACKAGE}-${pkg_format_lower}-resolved.yml" \
    "${pkg_format_lower}" \
    "${PKG_PATH}"

# DEB post-build signing (dpkg-sig)
if [[ "${PKG_FORMAT}" == "DEB" ]] && type -t sign_deb_package &>/dev/null; then
    sign_deb_package "${PKG_PATH}" "${PKG_FILENAME}"
fi

echo "${PKG_FORMAT} built: ${PKG_PATH}"
