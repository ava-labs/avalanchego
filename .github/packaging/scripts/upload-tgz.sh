#!/usr/bin/env bash

# e.g.,
# BUCKET=avalanchego-builds TGZ_ARCH=amd64 ./.github/packaging/scripts/upload-tgz.sh

# Uploads `*.tar.gz` and `*.tar.gz.sig` from build/tgz/ to S3. The GPG
# public key file is distributed via a separate S3 key location and is
# not republished per release.

set -euo pipefail

: "${BUCKET:?BUCKET must be set}"
: "${TGZ_ARCH:?TGZ_ARCH must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TGZ_DIR="${REPO_ROOT}/build/tgz"
S3_DEST="s3://${BUCKET}/linux/binaries/ubuntu/jammy/${TGZ_ARCH}/"

echo "=== Uploading tarballs from ${TGZ_DIR} to ${S3_DEST} ==="
find "${TGZ_DIR}" -maxdepth 1 \
    \( -name '*.tar.gz' -o -name '*.tar.gz.sig' \) \
    -exec aws s3 cp {} "${S3_DEST}" \;
