#!/usr/bin/env bash

# Build and sign a Linux package inside the container.

set -euo pipefail

: "${PACKAGE:?PACKAGE must be set (avalanchego or subnet-evm)}"
: "${VERSION:?VERSION must be set (semver without v prefix, e.g. 1.14.1)}"
: "${TAG:?TAG must be set (git tag, e.g. v1.14.1)}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set (x86_64 or aarch64)}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set (bind-mounted output dir)}"

: "${PKG_FORMAT:?PKG_FORMAT must be set (RPM or DEB)}"
pkg_format_lower="${PKG_FORMAT,,}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"

# shellcheck disable=SC1091
source "${PACKAGING_DIR}/scripts/lib-build-common.sh"

# Well-known paths referenced by nfpm configs
export NFPM_CHANGELOG="${REPO_ROOT}/build/nfpm-changelog.yml"
export NFPM_SIGNING_KEY="${REPO_ROOT}/build/gpg/signing-key.asc"

echo "=== Building ${PACKAGE} ${PKG_FORMAT} for ${PACKAGE_ARCH} (tag: ${TAG}) ==="

init_build_env
build_binary "${PACKAGE}"
generate_changelog "${VERSION}"

# ── GPG signing ───────────────────────────────────────────────────

GPG_KEY_FILE="${GPG_KEY_FILE:-}"
GPG_PUBLIC_KEY="${OUTPUT_DIR}/GPG-KEY-avalanchego"

# nfpm reads the signing passphrase from a packager-specific env var
# (NFPM_RPM_PASSPHRASE, NFPM_DEB_PASSPHRASE, ...); mirror our format-
# agnostic GPG_KEY_PASSPHRASE into the name nfpm expects.
nfpm_passphrase_var="NFPM_${PKG_FORMAT}_PASSPHRASE"
export "${nfpm_passphrase_var}=${GPG_KEY_PASSPHRASE:-}"

# Ephemeral keys use a known throwaway passphrase so local and CI builds
# exercise passphrase handling without release credentials.
if [[ -z "${GPG_KEY_FILE}" ]]; then
    use_ephemeral_gpg_passphrase "${nfpm_passphrase_var}"
fi

setup_gpg "${GPG_KEY_FILE}" "${GPG_PUBLIC_KEY}" "${PKG_FORMAT}"

# ── Package with nfpm ─────────────────────────────────────────────

export VERSION PACKAGE_ARCH BINARY_PATH

PKG_FILENAME="${PACKAGE}-${TAG}-${PACKAGE_ARCH}.${pkg_format_lower}"
PKG_PATH="${OUTPUT_DIR}/${PKG_FILENAME}"

run_nfpm_package \
    "${PACKAGING_DIR}/nfpm/${PACKAGE}-${pkg_format_lower}.yml" \
    "${REPO_ROOT}/build/${PACKAGE}-${pkg_format_lower}-resolved.yml" \
    "${pkg_format_lower}" \
    "${PKG_PATH}"

echo "${PKG_FORMAT} built: ${PKG_PATH}"
