#!/usr/bin/env bash

# e.g.,
# PACKAGING_TAG=v1.14.1 OUTPUT_DIR=$(pwd)/build/tgz ./.github/packaging/scripts/build-tgz.sh
# (add GPG_KEY_FILE=<path> GPG_PASSPHRASE=<pass> to also sign each tarball;
#  AVALANCHEGO_COMMIT=<sha> avoids the in-container git lookup)

# Builds and optionally signs linux tarballs for avalanchego and subnet-evm.

set -euo pipefail

: "${PACKAGING_TAG:?PACKAGING_TAG must be set}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set}"

REPO_ROOT="/build"
TAG="${PACKAGING_TAG}"

# Map uname -m to deb-style arch (aarch64 -> arm64).
host_arch=$(uname -m)
case "${host_arch}" in
    x86_64)        ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported arch: ${host_arch}" >&2; exit 1 ;;
esac

mkdir -p "${OUTPUT_DIR}"

# Remove stale tarballs, signatures, and the exported public key from
# any previous run. Tar would overwrite same-tag tarballs, but on
# persistent runners or after a failed cleanup, build/tgz can carry
# over tarballs from a different tag — and the workflow's S3 upload
# step matches *.tar.gz with a wildcard, so stale archives would be
# republished alongside the current release.
rm -f "${OUTPUT_DIR}"/*.tar.gz "${OUTPUT_DIR}"/*.tar.gz.sig "${OUTPUT_DIR}/GPG-KEY-avalanchego"

echo "=== Building tarballs for ${ARCH} (tag: ${TAG}) ==="

# ── Step 1: Build binaries ────────────────────────────────────────

# When AVALANCHEGO_COMMIT is unset, scripts/git_commit.sh falls back to
# `git rev-parse HEAD`. Mark the bind-mounted source tree as safe so
# that git can read .git/ across the UID boundary of the container.
# Skip the dance when AVALANCHEGO_COMMIT is set — the rev-parse won't
# run, and a worktree's gitfile pointing outside the bind-mount would
# make `git config` itself fail when it tries to resolve the gitdir.
if [[ -z "${AVALANCHEGO_COMMIT:-}" ]] && ! git -C "${REPO_ROOT}" rev-parse HEAD &>/dev/null; then
    git config --global --add safe.directory "${REPO_ROOT}"
fi

# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/constants.sh"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/git_commit.sh"

# shellcheck disable=SC2154
echo "Git commit: ${git_commit}"

# Disable Go's VCS auto-stamping — the commit hash is set explicitly via
# -ldflags inside scripts/build.sh.
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

# Tri-state behavior:
#   - GPG_KEY_FILE unset             → unsigned build (local dev OK)
#   - GPG_KEY_FILE set but empty     → CI signing secret missing/blank,
#                                       fail closed (no silent unsigned
#                                       release artifacts)
#   - GPG_KEY_FILE set and non-empty → sign each archive

if [[ -z "${GPG_KEY_FILE:-}" ]]; then
    echo "No GPG key provided, skipping tarball signing."
    sign_archive() { :; }
    GPG_SIGNING_ENABLED=false
elif [[ ! -s "${GPG_KEY_FILE}" ]]; then
    echo "ERROR: GPG_KEY_FILE is set (${GPG_KEY_FILE}) but the file is empty." >&2
    echo "       Refusing to produce unsigned release artifacts." >&2
    echo "       Verify that the GPG signing secret is configured." >&2
    exit 1
else
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

# ── Step 4: Export public key for validate-tgz.sh ─────────────────

if [[ "${GPG_SIGNING_ENABLED}" == "true" ]]; then
    PUB_KEY_FILE="${OUTPUT_DIR}/GPG-KEY-avalanchego"
    gpg --batch --armor --export "security@avalabs.org" > "${PUB_KEY_FILE}"
    echo "GPG public key exported to: ${PUB_KEY_FILE}"
fi

echo "=== Tarball build complete ==="
ls -la "${OUTPUT_DIR}"
