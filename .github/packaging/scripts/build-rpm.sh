#!/usr/bin/env bash

# Build and validate an RPM package inside the container.
#
# Required env vars:
#   PACKAGE        - "avalanchego" or "subnet-evm"
#   VERSION        - Semantic version without "v" prefix (e.g., "1.14.1")
#   TAG            - Git tag (e.g., "v1.14.1")
#   RPM_ARCH       - RPM architecture name ("x86_64" or "aarch64")
#   OUTPUT_DIR     - Directory for the output RPM (bind-mounted from host)
#
# Optional env vars:
#   RPM_GPG_KEY_FILE      - Path to GPG private key for signing
#   NFPM_RPM_PASSPHRASE   - Passphrase for the GPG key
#   AVALANCHEGO_COMMIT    - Git commit hash (auto-detected if not set)
#   PACKAGE_SIGNING_KMS_KEY_ID - AWS KMS key identifier for PKCS#11-backed signing

set -euo pipefail

: "${PACKAGE:?PACKAGE must be set (avalanchego or subnet-evm)}"
: "${VERSION:?VERSION must be set}"
: "${TAG:?TAG must be set}"
: "${RPM_ARCH:?RPM_ARCH must be set}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"

# Well-known paths referenced by nfpm configs
NFPM_CHANGELOG="${REPO_ROOT}/build/nfpm-changelog.yml"
NFPM_SIGNING_KEY="${REPO_ROOT}/build/gpg/signing-key.asc"

echo "=== Building ${PACKAGE} RPM for ${RPM_ARCH} (tag: ${TAG}) ==="

# ── Step 1: Build binary ──────────────────────────────────────────

# In CI, the bind-mounted source tree is owned by the host user. Mark it
# as safe so that git works inside the container (needed by older build
# scripts that resolve the commit hash via git rather than AVALANCHEGO_COMMIT).
if ! git -C "${REPO_ROOT}" rev-parse HEAD &>/dev/null; then
    git config --global --add safe.directory "${REPO_ROOT}"
fi

# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/constants.sh"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/git_commit.sh"

# shellcheck disable=SC2154
echo "Git commit: ${git_commit}"

# Disable Go's automatic VCS stamping — the commit hash is passed
# explicitly via AVALANCHEGO_COMMIT and -ldflags instead.
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"

case "${PACKAGE}" in
    avalanchego)
        echo "Building avalanchego..."
        "${REPO_ROOT}/scripts/build.sh"
        # shellcheck disable=SC2154
        BINARY_PATH="${avalanchego_path}"
        ;;
    subnet-evm)
        echo "Building subnet-evm..."
        # Source VM ID from constants.sh (canonical definition)
        SUBNET_EVM_VM_ID=$(
            grep '^DEFAULT_VM_ID=' "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh" \
            | cut -d'"' -f2
        )
        export SUBNET_EVM_VM_ID
        echo "Subnet-EVM VM ID: ${SUBNET_EVM_VM_ID}"

        SUBNET_EVM_BINARY="${REPO_ROOT}/build/subnet-evm"
        # Build from subnet-evm directory — build.sh uses relative glob "plugin/"*.go
        (cd "${REPO_ROOT}/graft/subnet-evm" && ./scripts/build.sh "${SUBNET_EVM_BINARY}")
        BINARY_PATH="${SUBNET_EVM_BINARY}"
        ;;
    *)
        echo "Unknown package: ${PACKAGE}" >&2
        exit 1
        ;;
esac

echo "Binary built at: ${BINARY_PATH}"

# ── Step 2: Generate changelog ────────────────────────────────────

cat > "${NFPM_CHANGELOG}" <<EOF
---
- semver: ${VERSION}
  date: $(date -u +%Y-%m-%dT%H:%M:%SZ)
  packager: Ava Labs <security@avalabs.org>
  changes:
    - note: "See https://github.com/ava-labs/avalanchego/releases/tag/v${VERSION}"
EOF

# ── Step 3: Set up GPG signing ────────────────────────────────────

GPG_WORKDIR="${REPO_ROOT}/build/gpg"
mkdir -p "${GPG_WORKDIR}"
GPG_PUBLIC_KEY="${OUTPUT_DIR}/RPM-GPG-KEY-avalanchego"
NFPM_CONFIG="${PACKAGING_DIR}/nfpm/${PACKAGE}.yml"
KMS_SIGNING_ENABLED=0

if [[ -n "${PACKAGE_SIGNING_KMS_KEY_ID:-}" ]]; then
    echo "Using AWS KMS-backed GPG signing"
    KMS_SIGNING_ENABLED=1
    export PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH="${GPG_PUBLIC_KEY}"
    "${PACKAGING_DIR}/scripts/setup-kms-gpg.sh"
    PACKAGE_SIGNING_GPG_FINGERPRINT="${PACKAGE_SIGNING_GPG_FINGERPRINT:-$(
        gpg --batch --with-colons --list-secret-keys 2>/dev/null \
            | awk -F: '$1 == "fpr" { print $10; exit }'
    )}"
    : "${PACKAGE_SIGNING_GPG_FINGERPRINT:?PACKAGE_SIGNING_GPG_FINGERPRINT could not be derived for KMS signing}"
    cat > "${HOME}/.rpmmacros" <<EOF
%_signature gpg
%_gpg_name ${PACKAGE_SIGNING_GPG_FINGERPRINT}
%__gpg /usr/bin/gpg
EOF
    NFPM_CONFIG="${PACKAGING_DIR}/nfpm/${PACKAGE}-unsigned.yml"
elif [[ -n "${RPM_GPG_KEY_FILE:-}" ]]; then
    echo "Using provided GPG key for signing"
    gpg --batch --import "${RPM_GPG_KEY_FILE}"
    # Copy to well-known path for nfpm config
    cp "${RPM_GPG_KEY_FILE}" "${NFPM_SIGNING_KEY}"
elif [[ -f "${NFPM_SIGNING_KEY}" ]]; then
    # Reuse ephemeral key from a previous build (e.g., avalanchego built before subnet-evm)
    echo "Reusing existing ephemeral GPG key"
    gpg --batch --import "${NFPM_SIGNING_KEY}"
    export NFPM_RPM_PASSPHRASE=""
else
    echo "Generating ephemeral GPG key for signing"

    gpg --batch --gen-key <<GPGEOF
%no-protection
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: AvalancheGo RPM Signing (ephemeral)
Name-Email: security@avalabs.org
Expire-Date: 1d
%commit
GPGEOF

    # Export private key to well-known path for nfpm
    gpg --batch --armor --export-secret-keys "security@avalabs.org" > "${NFPM_SIGNING_KEY}"
    export NFPM_RPM_PASSPHRASE=""
fi

if [[ "${KMS_SIGNING_ENABLED}" -eq 0 ]]; then
    # Export public key for verification
    gpg --batch --armor --export "security@avalabs.org" > "${GPG_PUBLIC_KEY}"
    echo "GPG public key exported to: ${GPG_PUBLIC_KEY}"
fi

# ── Step 4: Package with nfpm ─────────────────────────────────────

RPM_FILENAME="${PACKAGE}-${TAG}-${RPM_ARCH}.rpm"
RPM_PATH="${OUTPUT_DIR}/${RPM_FILENAME}"
mkdir -p "${OUTPUT_DIR}"

# Set binary path env var for nfpm config (contents use expand: true)
case "${PACKAGE}" in
    avalanchego) export AVALANCHEGO_BINARY="${BINARY_PATH}" ;;
    subnet-evm)  export SUBNET_EVM_BINARY="${BINARY_PATH}" ;;
esac

export VERSION RPM_ARCH

echo "Packaging ${RPM_FILENAME}..."
nfpm package \
    --config "${NFPM_CONFIG}" \
    --packager rpm \
    --target "${RPM_PATH}"

if [[ "${KMS_SIGNING_ENABLED}" -eq 1 ]]; then
    echo "Signing ${RPM_FILENAME} with rpmsign..."
    rpmsign --addsign "${RPM_PATH}"
fi

echo "RPM built: ${RPM_PATH}"
