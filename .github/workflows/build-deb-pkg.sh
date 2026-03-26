#!/usr/bin/env bash

set -euo pipefail

: "${PKG_ROOT:?PKG_ROOT must be set}"
: "${TAG:?TAG must be set}"
: "${ARCH:?ARCH must be set}"

DEBIAN_BASE_DIR="${PKG_ROOT}/debian"
AVALANCHE_BUILD_BIN_DIR="${DEBIAN_BASE_DIR}/usr/local/bin"
TEMPLATE=.github/workflows/debian/template
DEBIAN_CONF="${DEBIAN_BASE_DIR}/DEBIAN"
DEB_PATH="${PKG_ROOT}/avalanchego-${TAG}-${ARCH}.deb"

mkdir -p "$DEBIAN_BASE_DIR"
mkdir -p "$DEBIAN_CONF"
mkdir -p "$AVALANCHE_BUILD_BIN_DIR"

# Assume binaries are at default locations
cp ./build/avalanchego "$AVALANCHE_BUILD_BIN_DIR"
cp "$TEMPLATE/control" "$DEBIAN_CONF/control"

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
dpkg-deb --build debian "$(basename "${DEB_PATH}")"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  printf 'deb_path=%s\n' "${DEB_PATH}" >> "${GITHUB_OUTPUT}"
fi

echo "Built DEB: ${DEB_PATH}"
