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
# Deb files require codename disambiguation: after the deb producer's
# artifact-bundle rename (`debs-jammy-amd64`, `debs-noble-amd64`, etc.),
# the producer outputs arrive under SOURCE_DIR/debs-{codename}-{arch}/
# carrying identically-named files (`avalanchego-${TAG}-${arch}.deb`).
# resolve_source() maps each codename-suffixed target name to the source
# file by path substring, so each deb arrives in DEST_DIR with a unique
# basename matching the manifest.

set -euo pipefail

TAG="${1:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"
SOURCE_DIR="${2:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"
DEST_DIR="${3:?Usage: release-build-publish-set.sh <TAG> <SOURCE_DIR> <DEST_DIR>}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$DEST_DIR"

resolve_source() {
  local target="$1"
  case "$target" in
    avalanchego-*-jammy-amd64.deb)
      find "$SOURCE_DIR" -type f -path '*jammy*' -name "avalanchego-${TAG}-amd64.deb" | sort | head -n1
      ;;
    avalanchego-*-noble-amd64.deb)
      find "$SOURCE_DIR" -type f -path '*noble*' -name "avalanchego-${TAG}-amd64.deb" | sort | head -n1
      ;;
    avalanchego-*-jammy-arm64.deb)
      find "$SOURCE_DIR" -type f -path '*jammy*' -name "avalanchego-${TAG}-arm64.deb" | sort | head -n1
      ;;
    avalanchego-*-noble-arm64.deb)
      find "$SOURCE_DIR" -type f -path '*noble*' -name "avalanchego-${TAG}-arm64.deb" | sort | head -n1
      ;;
    *)
      find "$SOURCE_DIR" -type f -name "$target" | sort | head -n1
      ;;
  esac
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
