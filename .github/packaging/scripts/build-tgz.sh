#!/usr/bin/env bash

# Build and sign linux tarballs for avalanchego and subnet-evm inside the container.
#
# Required env vars:
#   PACKAGING_TAG       - Git tag (e.g., "v1.14.1")
#   OUTPUT_DIR          - Directory for the output tarballs (bind-mounted from host)
#
# Optional env vars:
#   GPG_KEY_FILE          - Path to GPG private key for signing
#   GPG_PASSPHRASE        - Passphrase for the GPG key
#   AVALANCHEGO_COMMIT    - Git commit hash (auto-detected if not set)
#
# Target architecture is derived from `uname -m` inside the container.
# The container runs at the platform pinned by the docker run --platform
# flag (host arch), so the produced filenames always match the binaries.

set -euo pipefail

: "${PACKAGING_TAG:?PACKAGING_TAG must be set}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set}"

REPO_ROOT="/build"
TAG="${PACKAGING_TAG}"

# Map uname -m to deb-style arch (aarch64 -> arm64). Computed inside the
# container, so it reflects the actual platform the binaries are built
# for, regardless of any caller-supplied env vars.
host_arch=$(uname -m)
case "${host_arch}" in
    x86_64)        ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported arch: ${host_arch}" >&2; exit 1 ;;
esac

mkdir -p "${OUTPUT_DIR}"

# Remove stale signatures and public key from a previous run. Tarballs
# are overwritten by tar, but .sig files and GPG-KEY-avalanchego are
# not — without this, an unsigned re-run after a signed run would
# leave .sig files that no longer match the freshly built tarballs.
rm -f "${OUTPUT_DIR}"/*.tar.gz.sig "${OUTPUT_DIR}/GPG-KEY-avalanchego"

echo "=== Building tarballs for ${ARCH} (tag: ${TAG}) ==="

# ── Step 1: Build binaries ────────────────────────────────────────

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

echo "Building avalanchego..."
"${REPO_ROOT}/scripts/build.sh"
# shellcheck disable=SC2154
AVALANCHEGO_BINARY="${avalanchego_path}"

echo "Building subnet-evm..."
SUBNET_EVM_BINARY="${REPO_ROOT}/build/subnet-evm"
# Build from subnet-evm directory — build.sh uses relative glob "plugin/"*.go
(cd "${REPO_ROOT}/graft/subnet-evm" && ./scripts/build.sh "${SUBNET_EVM_BINARY}")

echo "Binaries built:"
echo "  avalanchego: ${AVALANCHEGO_BINARY}"
echo "  subnet-evm:  ${SUBNET_EVM_BINARY}"

# ── Step 2: GPG setup ─────────────────────────────────────────────

# When GPG_KEY_FILE is set and non-empty, import the key into a temp
# GNUPGHOME and define a sign_archive() helper. Otherwise, define a
# no-op stub.

if [[ -n "${GPG_KEY_FILE:-}" && -s "${GPG_KEY_FILE}" ]]; then
    GNUPGHOME=$(mktemp -d)
    export GNUPGHOME
    trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "${GNUPGHOME}"' EXIT

    echo "Importing GPG key for tarball signing..."
    gpg --batch --import "${GPG_KEY_FILE}"

    sign_archive() {
        local archive="$1"
        echo "Signing ${archive}..."
        printf '%s' "${GPG_PASSPHRASE:-}" | gpg --batch --yes --detach-sign \
            --pinentry-mode loopback \
            --passphrase-fd 0 \
            "${archive}"
        echo "Verifying signature for ${archive}..."
        gpg --batch --verify "${archive}.sig" "${archive}"
    }

    GPG_SIGNING_ENABLED=true
else
    echo "No GPG key provided, skipping tarball signing."
    sign_archive() { :; }
    GPG_SIGNING_ENABLED=false
fi

# ── Step 3: Stage and tar each binary ─────────────────────────────

STAGE_DIR=$(mktemp -d)

stage_and_tar() {
    local package="$1"
    local binary="$2"
    local archive="${OUTPUT_DIR}/${package}-linux-${ARCH}-${TAG}.tar.gz"
    local stage="${STAGE_DIR}/${package}-${TAG}"

    mkdir -p "${stage}"
    cp "${binary}" "${stage}/"

    echo "Creating ${archive}..."
    (cd "${STAGE_DIR}" && tar -czvf "${archive}" "${package}-${TAG}")
    sign_archive "${archive}"
}

stage_and_tar "avalanchego" "${AVALANCHEGO_BINARY}"
stage_and_tar "subnet-evm" "${SUBNET_EVM_BINARY}"

rm -rf "${STAGE_DIR}"

# ── Step 4: Export public key (for validation container only) ─────
#
# The public key is used by validate-tgz.sh to verify signatures in a
# fresh container. It is NOT uploaded to S3 or as a GitHub artifact —
# the public key is distributed via the existing S3 location only.

if [[ "${GPG_SIGNING_ENABLED}" == "true" ]]; then
    PUB_KEY_FILE="${OUTPUT_DIR}/GPG-KEY-avalanchego"
    gpg --batch --armor --export "security@avalabs.org" > "${PUB_KEY_FILE}"
    echo "GPG public key exported to: ${PUB_KEY_FILE} (validation use only)"
fi

echo "=== Tarball build complete ==="
ls -la "${OUTPUT_DIR}"
