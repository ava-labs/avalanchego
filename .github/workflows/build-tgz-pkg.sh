#!/usr/bin/env bash

set -euo pipefail

# ── GPG setup ────────────────────────────────────────────────────
#
# When GPG_KEY_FILE is set and non-empty, import the key and define
# a sign_tarball() helper. Otherwise, define a no-op stub.

if [[ -n "${GPG_KEY_FILE:-}" && -s "${GPG_KEY_FILE}" ]]; then
    echo "Importing GPG key for tarball signing..."
    gpg --batch --import "${GPG_KEY_FILE}"

    sign_tarball() {
        local tarball="$1"
        echo "Signing ${tarball}..."
        gpg --batch --yes --detach-sign \
            --pinentry-mode loopback \
            --passphrase "${GPG_PASSPHRASE:-}" \
            "${tarball}"
        echo "Verifying signature for ${tarball}..."
        gpg --batch --verify "${tarball}.sig" "${tarball}"
    }

    GPG_SIGNING_ENABLED=true
else
    echo "No GPG key provided, skipping tarball signing."
    sign_tarball() { :; }
    GPG_SIGNING_ENABLED=false
fi

# ── Build avalanchego tarball ────────────────────────────────────

AVALANCHE_ROOT=$PKG_ROOT/avalanchego-$TAG

mkdir -p "$AVALANCHE_ROOT"

OK=$(cp ./build/avalanchego "$AVALANCHE_ROOT")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build avalanchego tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
tar -czvf "avalanchego-linux-$ARCH-$TAG.tar.gz" "avalanchego-$TAG"
sign_tarball "avalanchego-linux-$ARCH-$TAG.tar.gz"
aws s3 cp "avalanchego-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
if [[ "$GPG_SIGNING_ENABLED" == "true" ]]; then
    aws s3 cp "avalanchego-linux-$ARCH-$TAG.tar.gz.sig" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
fi

# ── Build subnet-evm tarball ─────────────────────────────────────

echo "Build subnet-evm tgz package..."
SUBNET_EVM_ROOT=$PKG_ROOT/subnet-evm-$TAG
mkdir -p "$SUBNET_EVM_ROOT"
cp "${GITHUB_WORKSPACE:-$PWD}/build/subnet-evm" "$SUBNET_EVM_ROOT/"
cd "$PKG_ROOT"
tar -czvf "subnet-evm-linux-$ARCH-$TAG.tar.gz" "subnet-evm-$TAG"
sign_tarball "subnet-evm-linux-$ARCH-$TAG.tar.gz"
aws s3 cp "subnet-evm-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
if [[ "$GPG_SIGNING_ENABLED" == "true" ]]; then
    aws s3 cp "subnet-evm-linux-$ARCH-$TAG.tar.gz.sig" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
fi
