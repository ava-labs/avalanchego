#!/usr/bin/env bash

# Build and sign a Linux package with nfpm inside the container.

set -euo pipefail

: "${PACKAGE:?PACKAGE must be set (avalanchego or subnet-evm)}"
: "${VERSION:?VERSION must be set (semantic version without v prefix, e.g. 1.14.1)}"
: "${TAG:?TAG must be set (git tag, e.g. v1.14.1)}"
: "${PACKAGE_ARCH:?PACKAGE_ARCH must be set (x86_64/aarch64 for RPM, amd64/arm64 for DEB)}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set (bind-mounted directory for the produced package)}"
: "${NFPM_PACKAGER:?NFPM_PACKAGER must be set (rpm or deb)}"

NFPM_PACKAGER="${NFPM_PACKAGER,,}"
pkg_format_upper="${NFPM_PACKAGER^^}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"
NFPM_CONFIG_TEMPLATE="${PACKAGING_DIR}/nfpm/${PACKAGE}-${NFPM_PACKAGER}.yml"
NFPM_CONFIG_RESOLVED="${REPO_ROOT}/build/${PACKAGE}-${NFPM_PACKAGER}-resolved.yml"
if [[ ! -f "${NFPM_CONFIG_TEMPLATE}" ]]; then
    echo "Unknown nfpm packager or package: ${NFPM_PACKAGER} / ${PACKAGE}" >&2
    exit 1
fi

# shellcheck disable=SC1091
source "${PACKAGING_DIR}/scripts/lib-build-common.sh"

# Well-known paths referenced by nfpm configs
export NFPM_CHANGELOG="${REPO_ROOT}/build/nfpm-changelog.yml"
export NFPM_SIGNING_KEY="${REPO_ROOT}/build/gpg/signing-key.asc"
GPG_KEY_FILE="${GPG_KEY_FILE:-}"

echo "=== Building ${PACKAGE} ${pkg_format_upper} for ${PACKAGE_ARCH} (tag: ${TAG}) ==="

init_build_env
build_binary "${PACKAGE}"
generate_changelog "${VERSION}"

# ── GPG signing ───────────────────────────────────────────────────

GPG_PUBLIC_KEY="${OUTPUT_DIR}/GPG-KEY-avalanchego"

# nfpm reads the signing passphrase from a packager-specific env var
# (NFPM_RPM_PASSPHRASE, NFPM_DEB_PASSPHRASE, ...); mirror our format-
# agnostic GPG_KEY_PASSPHRASE into the name nfpm expects.
nfpm_passphrase_var="NFPM_${pkg_format_upper}_PASSPHRASE"
export "${nfpm_passphrase_var}=${GPG_KEY_PASSPHRASE:-}"

# Ephemeral keys use a known throwaway passphrase so local and CI builds
# exercise passphrase handling without release credentials.
if [[ -z "${GPG_KEY_FILE}" ]]; then
    use_ephemeral_gpg_passphrase "${nfpm_passphrase_var}"
fi

setup_gpg "${GPG_KEY_FILE}" "${GPG_PUBLIC_KEY}" "${pkg_format_upper}"

# ── Package with nfpm ─────────────────────────────────────────────

export VERSION PACKAGE_ARCH BINARY_PATH

PKG_FILENAME="${PACKAGE}-${TAG}-${PACKAGE_ARCH}.${NFPM_PACKAGER}"
PKG_PATH="${OUTPUT_DIR}/${PKG_FILENAME}"

run_nfpm_package \
    "${NFPM_CONFIG_TEMPLATE}" \
    "${NFPM_CONFIG_RESOLVED}" \
    "${NFPM_PACKAGER}" \
    "${PKG_PATH}"

echo "${pkg_format_upper} built: ${PKG_PATH}"
