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

# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/constants.sh"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/git_commit.sh"

# shellcheck disable=SC2154
echo "Git commit: ${git_commit}"

# Disable Go's automatic VCS stamping — the bind-mounted .git is owned by
# the host user, causing git to fail inside the container. The commit hash
# is passed explicitly via AVALANCHEGO_COMMIT and -ldflags instead.
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

"${PACKAGING_DIR}/scripts/extract-changelog.sh" "${VERSION}" "${REPO_ROOT}/RELEASES.md" "${NFPM_CHANGELOG}"

# ── Step 3: Set up GPG signing ────────────────────────────────────

GPG_WORKDIR="${REPO_ROOT}/build/gpg"
mkdir -p "${GPG_WORKDIR}"
GPG_PUBLIC_KEY="${OUTPUT_DIR}/RPM-GPG-KEY-avalanchego"

if [[ -n "${RPM_GPG_KEY_FILE:-}" ]]; then
    echo "Using provided GPG key for signing"
    gpg --batch --import "${RPM_GPG_KEY_FILE}"
    # Copy to well-known path for nfpm config
    cp "${RPM_GPG_KEY_FILE}" "${NFPM_SIGNING_KEY}"
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

# Export public key for verification
gpg --batch --armor --export "security@avalabs.org" > "${GPG_PUBLIC_KEY}"
echo "GPG public key exported to: ${GPG_PUBLIC_KEY}"

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
    --config "${PACKAGING_DIR}/nfpm/${PACKAGE}.yml" \
    --packager rpm \
    --target "${RPM_PATH}"

echo "RPM built: ${RPM_PATH}"

# ── Step 5: Validate ──────────────────────────────────────────────

echo "=== Validating ${RPM_FILENAME} ==="

# Import public key and verify signature
rpm --import "${GPG_PUBLIC_KEY}"
rpm -K "${RPM_PATH}"
echo "Signature verification passed"

# Install the RPM
rpm -ivh "${RPM_PATH}"
echo "Installation succeeded"

# Smoke test
case "${PACKAGE}" in
    avalanchego)
        output=$(/var/opt/avalanchego/bin/avalanchego --version)
        echo "avalanchego --version: ${output}"

        if [[ "${output}" != avalanchego/* ]]; then
            echo "ERROR: --version output does not start with 'avalanchego/'" >&2
            exit 1
        fi

        # Verify git commit hash is present in output
        # shellcheck disable=SC2154
        short_commit="${git_commit::8}"
        if [[ "${output}" != *"commit=${short_commit}"* ]]; then
            echo "ERROR: --version output does not contain expected commit=${short_commit}" >&2
            echo "Output: ${output}" >&2
            exit 1
        fi

        echo "Smoke test passed: binary runs, correct commit"
        ;;
    subnet-evm)
        plugin_path="/var/opt/avalanchego/plugins/${SUBNET_EVM_VM_ID}"
        if [[ ! -x "${plugin_path}" ]]; then
            echo "ERROR: plugin not found or not executable at ${plugin_path}" >&2
            exit 1
        fi
        echo "Smoke test passed: plugin installed and executable"
        ;;
esac

echo "=== ${PACKAGE} RPM build and validation complete ==="
