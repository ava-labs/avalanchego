#!/usr/bin/env bash

# Upload built linux tarballs and detached signatures to S3.
#
# Required env vars:
#   BUCKET     - Target S3 bucket name
#   TGZ_ARCH   - Tarball architecture suffix in the S3 path ("amd64" or "arm64")
#
# Uploads only `*.tar.gz` and `*.tar.gz.sig` from build/tgz/. Explicitly
# excludes `GPG-KEY-avalanchego` — the public key is distributed via the
# existing S3 key location, not republished alongside each release.

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
