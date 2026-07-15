#!/usr/bin/env bash
# release-build-publish-set.sh — assemble publish-set from producer outputs.
#
# Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>
#
# For each entry in release-expected-manifest.sh's output, find the matching
# source file under SOURCE_DIR (with deb-codename disambiguation by path
# substring) and copy it to DEST_DIR under the manifest's target basename.
# Fails (exit 1) on any missing entry, listing the missing manifest targets
# and the downloaded inventory.
#
# Deb files need codename handling: the unified producer builds one
# codename-agnostic .deb per arch and uploads per-arch bundles
# (SOURCE_DIR/debs-amd64/, SOURCE_DIR/debs-arm64/), each carrying avalanchego
# and subnet-evm plus the GPG key. resolve_source() maps every codename-embedded
# manifest target (jammy/noble) back to its single per-arch file; the cp below
# then renames it, so each per-arch binary is published under both codenames.

set -euo pipefail

TAG="${1:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"
SOURCE_DIR="${2:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"
DEST_DIR="${3:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$DEST_DIR"

resolve_source() {
  local target="$1"
  # A codename-embedded deb target (<pkg>-<tag>-<jammy|noble>-<arch>.deb) has no
  # codename in the built file: strip it and match the single per-arch binary
  # <pkg>-<tag>-<arch>.deb in its debs-<arch> bundle. Everything else (rpms,
  # tarballs, macOS zips, .sig) matches the manifest basename verbatim.
  if [[ "$target" =~ ^(avalanchego|subnet-evm)-.*-(jammy|noble)-(amd64|arm64)\.deb$ ]]; then
    find "$SOURCE_DIR" -type f \
      -name "${BASH_REMATCH[1]}-${TAG}-${BASH_REMATCH[3]}.deb" | sort | head -n1
  else
    find "$SOURCE_DIR" -type f -name "$target" | sort | head -n1
  fi
}

missing=()
while IFS= read -r expected; do
  [[ -z "$expected" ]] && continue
  match="$(resolve_source "$expected")"
  if [[ -z "$match" ]]; then
    missing+=("$expected")
    continue
  fi
  cp "$match" "${DEST_DIR}/${expected}"
done <<<"$("${HERE}/release-expected-manifest.sh" "$TAG")"

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "Error: expected artifacts missing from producer outputs:" >&2
  printf '  %s\n' "${missing[@]}" >&2
  echo "Downloaded inventory (${SOURCE_DIR}):" >&2
  find "$SOURCE_DIR" -type f | sort >&2
  exit 1
fi
echo "Publish-set assembled:"
ls -la "$DEST_DIR/"
