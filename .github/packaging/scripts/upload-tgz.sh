#!/usr/bin/env bash

# e.g.,
# BUCKET=avalanchego-builds TGZ_ARCH=amd64 ./.github/packaging/scripts/upload-tgz.sh

# Uploads `*.tar.gz` and `*.tar.gz.sig` from build/tgz/ to S3. The GPG
# public key file is distributed via a separate S3 key location and is
# not republished per release.

set -euo pipefail

: "${BUCKET:?BUCKET must be set}"
: "${TGZ_ARCH:?TGZ_ARCH must be set}"
: "${TGZ_RELEASE:?TGZ_RELEASE must be set (e.g. jammy)}"
: "${TAG:?TAG must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TGZ_DIR="${REPO_ROOT}/build/tgz"
S3_DEST="s3://${BUCKET}/linux/binaries/ubuntu/${TGZ_RELEASE}/${TGZ_ARCH}/"

echo "=== Uploading tarballs from ${TGZ_DIR} to ${S3_DEST} ==="

upload_if_exists() {
    local f="$1"
    if [[ -f "${f}" ]]; then
        aws s3 cp "${f}" "${S3_DEST}"
    else
        echo "Skipping ${f##*/} (not present — unsigned build)"
    fi
}

# Tarballs are required.
aws s3 cp "${TGZ_DIR}/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz" "${S3_DEST}"
aws s3 cp "${TGZ_DIR}/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz"   "${S3_DEST}"
# Signatures are present only when build-tgz.sh ran with a signing key.
upload_if_exists "${TGZ_DIR}/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz.sig"
upload_if_exists "${TGZ_DIR}/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz.sig"
