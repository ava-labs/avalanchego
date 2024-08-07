#!/usr/bin/env bash

set -euo pipefail

DEBIAN_BASE_DIR=$PKG_ROOT/debian
CAMINO_BUILD_BIN_DIR=$DEBIAN_BASE_DIR/usr/local/bin
TEMPLATE=.github/workflows/debian/template
DEBIAN_CONF=$DEBIAN_BASE_DIR/DEBIAN

mkdir -p "$DEBIAN_BASE_DIR"
mkdir -p "$DEBIAN_CONF"
mkdir -p "$CAMINO_BUILD_BIN_DIR"

# Assume binaries are at default locations
OK=$(cp ./build/caminogo "$CAMINO_BUILD_BIN_DIR")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi

OK=$(cp $TEMPLATE/control "$DEBIAN_CONF"/control)
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi

echo "Build debian package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
VER=$TAG
if [[ $TAG =~ ^v ]]; then
  VER=$(echo "$TAG" | tr -d 'v')
fi
NEW_VERSION_STRING="Version: $VER"
NEW_ARCH_STRING="Architecture: $ARCH"
sed -i "s/Version.*/$NEW_VERSION_STRING/g" debian/DEBIAN/control
sed -i "s/Architecture.*/$NEW_ARCH_STRING/g" debian/DEBIAN/control
dpkg-deb --build debian "caminogo-$TAG-$ARCH.deb"
#gsutil cp "caminogogo-$TAG-$ARCH.deb" "gs://$BUCKET/linux/debs/ubuntu/$RELEASE/$ARCH/"
