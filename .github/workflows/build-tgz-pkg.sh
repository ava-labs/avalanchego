#!/usr/bin/env bash

set -euo pipefail

CAMINO_ROOT=$PKG_ROOT/caminogo-$TAG

mkdir -p "$CAMINO_ROOT"

OK=$(cp ./build/caminogo "$CAMINO_ROOT")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
tar -czvf "caminogo-linux-$ARCH-$TAG.tar.gz" "caminogo-$TAG"
#gsutil cp "caminogo-linux-$ARCH-$TAG.tar.gz" "gs://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
