#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_ROOT=$PKG_ROOT/avalanchego-$TAG

mkdir -p "$AVALANCHE_ROOT"

OK=$(cp ./build/avalanchego "$AVALANCHE_ROOT")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
tar -czvf "avalanchego-linux-$ARCH-$TAG.tar.gz" "avalanchego-$TAG"
aws s3 cp "avalanchego-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
