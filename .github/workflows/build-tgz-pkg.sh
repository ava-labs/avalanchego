#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_ROOT=$PKG_ROOT/metalgo-$TAG

mkdir -p "$AVALANCHE_ROOT"

OK=$(cp ./build/metalgo "$AVALANCHE_ROOT")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build metalgo tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
tar -czvf "metalgo-linux-$ARCH-$TAG.tar.gz" "metalgo-$TAG"
aws s3 cp "metalgo-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"

echo "Build subnet-evm tgz package..."
SUBNET_EVM_ROOT=$PKG_ROOT/subnet-evm-$TAG
mkdir -p "$SUBNET_EVM_ROOT"
cp "${GITHUB_WORKSPACE:-$PWD}/build/subnet-evm" "$SUBNET_EVM_ROOT/"
cd "$PKG_ROOT"
tar -czvf "subnet-evm-linux-$ARCH-$TAG.tar.gz" "subnet-evm-$TAG"
aws s3 cp "subnet-evm-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
