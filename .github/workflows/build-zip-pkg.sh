#!/usr/bin/env bash

set -euo pipefail

# ── GPG setup ────────────────────────────────────────────────────
#
# When GPG_KEY_FILE is set and non-empty, import the key and define
# a sign_archive() helper. Otherwise, define a no-op stub.

if [[ -n "${GPG_KEY_FILE:-}" && -s "${GPG_KEY_FILE}" ]]; then
    GNUPGHOME=$(mktemp -d)
    export GNUPGHOME
    trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "${GNUPGHOME}"' EXIT

    echo "Importing GPG key for archive signing..."
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
    echo "No GPG key provided, skipping archive signing."
    sign_archive() { :; }
    GPG_SIGNING_ENABLED=false
fi

# ── Build avalanchego zip ────────────────────────────────────────

echo "Build avalanchego zip package..."
echo "Tag: $TAG"
7z a "avalanchego-macos-${TAG}.zip" build/avalanchego
sign_archive "avalanchego-macos-${TAG}.zip"
aws s3 cp "avalanchego-macos-${TAG}.zip" "s3://${BUCKET}/macos/"
if [[ "$GPG_SIGNING_ENABLED" == "true" ]]; then
    aws s3 cp "avalanchego-macos-${TAG}.zip.sig" "s3://${BUCKET}/macos/"
fi

# ── Build subnet-evm zip ────────────────────────────────────────

echo "Build subnet-evm zip package..."
7z a "subnet-evm-macos-${TAG}.zip" build/subnet-evm
sign_archive "subnet-evm-macos-${TAG}.zip"
aws s3 cp "subnet-evm-macos-${TAG}.zip" "s3://${BUCKET}/macos/"
if [[ "$GPG_SIGNING_ENABLED" == "true" ]]; then
    aws s3 cp "subnet-evm-macos-${TAG}.zip.sig" "s3://${BUCKET}/macos/"
fi
