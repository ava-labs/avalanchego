#!/usr/bin/env bash

set -euo pipefail

if ! command -v nfpm &> /dev/null; then
  echo "Installing nfpm..."
  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
fi

echo "Build RPM packages..."
echo "Tag: $TAG"

# RPM versions must not start with letters - strip 'v' prefix
VERSION=$TAG
if [[ $VERSION =~ ^v ]]; then
  VERSION=${VERSION#v}
fi
export VERSION
export ARCH

NFPM_CONFIG_DIR=.github/workflows/nfpm
OUTPUT_DIR=$PKG_ROOT

mkdir -p "$OUTPUT_DIR"

echo "Building avalanchego RPM..."
nfpm package \
  --config "$NFPM_CONFIG_DIR/avalanchego.yml" \
  --packager rpm \
  --target "$OUTPUT_DIR/avalanchego-${TAG}-${ARCH}.rpm"

echo "Building subnet-evm RPM..."
nfpm package \
  --config "$NFPM_CONFIG_DIR/subnet-evm.yml" \
  --packager rpm \
  --target "$OUTPUT_DIR/subnet-evm-${TAG}-${ARCH}.rpm"

echo "Uploading RPMs to S3..."
aws s3 cp "$OUTPUT_DIR/avalanchego-${TAG}-${ARCH}.rpm" "s3://${BUCKET}/linux/rpms/${ARCH}/"
aws s3 cp "$OUTPUT_DIR/subnet-evm-${TAG}-${ARCH}.rpm" "s3://${BUCKET}/linux/rpms/${ARCH}/"
