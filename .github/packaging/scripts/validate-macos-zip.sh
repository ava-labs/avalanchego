#!/usr/bin/env bash

# Post-build validation of macOS zip packages.
#
# Verifies the artifacts produced by build-macos-zip.sh by spinning a
# fresh container, importing the exported GPG public key, verifying each
# detached signature against its zip, listing the zip contents, and
# running rcodesign verify on the extracted Mach-O binaries.

set -euo pipefail

: "${TAG:?TAG must be set (Git tag, e.g. v0.0.0)}"
: "${PACKAGING_DOCKER_IMAGE:?PACKAGING_DOCKER_IMAGE must be set (validator image)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
MACOS_DIR="${REPO_ROOT}/build/macos"
SCRIPTS_DIR="${REPO_ROOT}/.github/packaging/scripts"

# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib-build-common.sh"

assert_files_exist "${MACOS_DIR}" \
    "avalanchego-macos-${TAG}.zip" \
    "avalanchego-macos-${TAG}.zip.sig" \
    "subnet-evm-macos-${TAG}.zip" \
    "subnet-evm-macos-${TAG}.zip.sig" \
    "GPG-KEY-avalanchego.asc"

echo "=== Validating macOS zips in fresh container ==="
# The validator container reuses the builder image because the
# verification tools (gpg, 7z, rcodesign, jq) are the same toolchain
# that the workflow ships in its sign-publish job. Each docker run is
# fresh, so no builder-side state leaks in.
docker run --rm \
    -v "${MACOS_DIR}:/macos:ro" \
    -e TAG="${TAG}" \
    "${PACKAGING_DOCKER_IMAGE}" \
    bash -euxc '
        WORK=$(mktemp -d)
        cd "${WORK}"

        # ── GPG signature verification ────────────────────────────
        # Use an isolated GNUPGHOME so we exercise the same trust path
        # a downstream consumer would: import the public key, verify.
        export GNUPGHOME=$(mktemp -d)
        gpg --batch --import /macos/GPG-KEY-avalanchego.asc

        for pkg in avalanchego subnet-evm; do
            echo "Verifying GPG signature for ${pkg}..."
            gpg --batch --verify \
                "/macos/${pkg}-macos-${TAG}.zip.sig" \
                "/macos/${pkg}-macos-${TAG}.zip"
        done

        # ── Zip content sanity ────────────────────────────────────
        # build-zip-pkg.sh calls "7z a ... build/<pkg>" so the
        # internal entry preserves the build/ prefix.
        for pkg in avalanchego subnet-evm; do
            echo "Listing contents of ${pkg}-macos-${TAG}.zip..."
            7z l "/macos/${pkg}-macos-${TAG}.zip" | grep -q "build/${pkg}\$"
        done

        # ── Apple signature parseability (rcodesign verify) ───────
        # Self-signed signatures verify structurally; trust chain is
        # not checked by rcodesign verify (it would be by Apple notary).
        for pkg in avalanchego subnet-evm; do
            echo "Extracting and rcodesign-verifying ${pkg}..."
            rm -rf build
            7z x "/macos/${pkg}-macos-${TAG}.zip" -y >/dev/null
            file "build/${pkg}" | grep -q "Mach-O"
            rcodesign verify "build/${pkg}"
        done

        # API key JSON shape check happens inline in build-macos-zip.sh,
        # against the ephemeral STAGE copy — the file holds a private
        # signing key when real notarization creds are provided, so it
        # never makes it to the bind-mounted OUTPUT_DIR.

        echo "All macOS zip validations passed."
    '

echo "=== macOS zip validation complete ==="
