#!/usr/bin/env bash

# e.g.,
# BUCKET=avalanchego-builds TGZ_ARCH=amd64 ./.github/packaging/scripts/upload-tgz.sh

# Uploads the exact expected `.tar.gz` and `.tar.gz.sig` files from
# build/tgz/ to S3. The GPG public key file is used to verify signatures
# before upload, but it is distributed via a separate S3 key location and
# is not republished per release.

set -euo pipefail

: "${BUCKET:?BUCKET must be set}"
: "${TGZ_ARCH:?TGZ_ARCH must be set}"
: "${TGZ_RELEASE:?TGZ_RELEASE must be set (e.g. jammy)}"
: "${TAG:?TAG must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TGZ_DIR="${REPO_ROOT}/build/tgz"
S3_DEST="s3://${BUCKET}/linux/binaries/ubuntu/${TGZ_RELEASE}/${TGZ_ARCH}/"

echo "=== Uploading tarballs from ${TGZ_DIR} to ${S3_DEST} ==="

VERIFY_GNUPGHOME=""
cleanup_on_exit() {
    if [[ -n "${VERIFY_GNUPGHOME}" ]]; then
        gpgconf --kill gpg-agent 2>/dev/null || true
        rm -rf "${VERIFY_GNUPGHOME}"
    fi
}
trap cleanup_on_exit EXIT

require_file() {
    local f="$1"
    if [[ ! -f "${f}" ]]; then
        echo "ERROR: expected file not found: ${f}" >&2
        exit 1
    fi
}

verify_signature() {
    local archive="$1"
    local signature="${archive}.sig"

    echo "Verifying ${signature##*/}..."
    if ! gpg --batch --verify "${signature}" "${archive}"; then
        echo "ERROR: signature verification failed for ${archive##*/}" >&2
        exit 1
    fi
}

AVALANCHEGO_TGZ="${TGZ_DIR}/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz"
SUBNET_EVM_TGZ="${TGZ_DIR}/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz"
PUBLIC_KEY="${TGZ_DIR}/GPG-KEY-avalanchego"

for f in \
    "${AVALANCHEGO_TGZ}" \
    "${AVALANCHEGO_TGZ}.sig" \
    "${SUBNET_EVM_TGZ}" \
    "${SUBNET_EVM_TGZ}.sig" \
    "${PUBLIC_KEY}"
do
    require_file "${f}"
done

VERIFY_GNUPGHOME=$(mktemp -d)
export GNUPGHOME="${VERIFY_GNUPGHOME}"
gpg --batch --import "${PUBLIC_KEY}"
verify_signature "${AVALANCHEGO_TGZ}"
verify_signature "${SUBNET_EVM_TGZ}"

aws s3 cp "${AVALANCHEGO_TGZ}" "${S3_DEST}"
aws s3 cp "${SUBNET_EVM_TGZ}"   "${S3_DEST}"
aws s3 cp "${AVALANCHEGO_TGZ}.sig" "${S3_DEST}"
aws s3 cp "${SUBNET_EVM_TGZ}.sig"   "${S3_DEST}"
