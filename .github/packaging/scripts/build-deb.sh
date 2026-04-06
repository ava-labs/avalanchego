#!/usr/bin/env bash

# Build and sign a DEB package inside the container.
#
# Required env vars:
#   PACKAGE        - "avalanchego" or "subnet-evm"
#   VERSION        - Semantic version without "v" prefix (e.g., "1.14.1")
#   TAG            - Git tag (e.g., "v1.14.1")
#   PACKAGE_ARCH   - DEB architecture name ("amd64" or "arm64")
#   OUTPUT_DIR     - Directory for the output DEB (bind-mounted from host)
#
# Optional env vars:
#   DEB_GPG_KEY_FILE      - Path to GPG private key for signing
#   NFPM_DEB_PASSPHRASE   - Passphrase for the GPG key (cached in gpg-agent for dpkg-sig)
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

echo "=== Building ${PACKAGE} DEB for ${PACKAGE_ARCH} (tag: ${TAG}) ==="

init_build_env
build_binary "${PACKAGE}"
generate_changelog "${VERSION}"

# ── GPG signing (DEB-specific: gpg-agent for dpkg-sig) ───────────

GPG_PUBLIC_KEY="${OUTPUT_DIR}/DEB-GPG-KEY-avalanchego"

# Configure gpg-agent for non-interactive signing. dpkg-sig delegates to gpg,
# which needs the passphrase available via gpg-agent. allow-preset-passphrase
# lets us cache the passphrase so dpkg-sig can sign without prompting.
GPG_AGENT_CONF="${HOME}/.gnupg/gpg-agent.conf"
mkdir -p "$(dirname "${GPG_AGENT_CONF}")"
if ! grep -q allow-preset-passphrase "${GPG_AGENT_CONF}" 2>/dev/null; then
    echo "allow-preset-passphrase" >> "${GPG_AGENT_CONF}"
    gpgconf --kill gpg-agent 2>/dev/null || true
fi

setup_gpg "${DEB_GPG_KEY_FILE:-}" "${GPG_PUBLIC_KEY}" "DEB"

# Cache passphrase in gpg-agent for dpkg-sig non-interactive signing
if [[ -n "${DEB_GPG_KEY_FILE:-}" && -n "${NFPM_DEB_PASSPHRASE:-}" ]]; then
    GPG_PRESET_PASS="$(gpgconf --list-dirs libexecdir)/gpg-preset-passphrase"
    KEYGRIPS=$(gpg --batch --with-colons --with-keygrip --list-secret-keys "security@avalabs.org" \
        | awk -F: '$1 == "grp" { print $10 }')
    for kg in ${KEYGRIPS}; do
        echo "${NFPM_DEB_PASSPHRASE}" | "${GPG_PRESET_PASS}" --preset "${kg}"
    done
    echo "GPG passphrase cached in gpg-agent"
fi

# ── Package with nfpm ─────────────────────────────────────────────

case "${PACKAGE}" in
    avalanchego) export AVALANCHEGO_BINARY="${BINARY_PATH}" ;;
    subnet-evm)  export SUBNET_EVM_BINARY="${BINARY_PATH}" ;;
esac

export VERSION PACKAGE_ARCH

DEB_FILENAME="${PACKAGE}-${TAG}-${PACKAGE_ARCH}.deb"
DEB_PATH="${OUTPUT_DIR}/${DEB_FILENAME}"

run_nfpm_package \
    "${PACKAGING_DIR}/nfpm/${PACKAGE}-deb.yml" \
    "${REPO_ROOT}/build/${PACKAGE}-deb-resolved.yml" \
    deb \
    "${DEB_PATH}"

# ── Sign with dpkg-sig ───────────────────────────────────────────
# nfpm's Go openpgp signatures are incompatible with dpkg-sig --verify,
# so we sign post-build with dpkg-sig itself for verifiable signatures.

GPG_FINGERPRINT=$(gpg --batch --with-colons --list-secret-keys "security@avalabs.org" 2>/dev/null \
    | awk -F: '$1 == "fpr" { print $10; exit }')
echo "Signing ${DEB_FILENAME} with GPG fingerprint ${GPG_FINGERPRINT}..."
dpkg-sig --sign builder -k "${GPG_FINGERPRINT}" "${DEB_PATH}"

echo "DEB built and signed: ${DEB_PATH}"
