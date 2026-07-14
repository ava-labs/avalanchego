#!/usr/bin/env bash

# Build and sign macOS zip archives inside the macos-zip-builder container.
#
# Exercises the same packaging path as the build-macos-release workflow's
# sign-publish job, but with self-generated credentials where Apple's real
# ones aren't available locally. Produces:
#   ${OUTPUT_DIR}/{avalanchego,subnet-evm}-macos-${TAG}.zip{,.sig}
#   ${OUTPUT_DIR}/GPG-KEY-avalanchego.asc

set -euo pipefail

: "${TAG:?TAG must be set (git tag, e.g. v0.0.0)}"
: "${OUTPUT_DIR:?OUTPUT_DIR must be set (bind-mounted output dir)}"

REPO_ROOT="/build"
PACKAGING_DIR="${REPO_ROOT}/.github/packaging"

# shellcheck disable=SC1091
source "${PACKAGING_DIR}/scripts/lib-build-common.sh"

# Self-signed P12 password used when MACOS_SIGNING_PASSWORD is not provided.
readonly EPHEMERAL_P12_PASSWORD="avalanchego-ephemeral-p12-password"

# Throwaway issuer/key IDs used when real notarization credentials aren't
# provided. rcodesign encode-app-store-connect-api-key does not validate
# the issuer or key id format — it just wraps them into a JSON blob.
readonly EPHEMERAL_NOTARIZATION_KEY_ID="EPHEMERAL_KEY_ID"
readonly EPHEMERAL_NOTARIZATION_ISSUER="00000000-0000-0000-0000-000000000000"

# Mark the bind-mounted source tree as git-safe so the script can read
# the production build-zip-pkg.sh from the repo. We do NOT call the
# project's full init_build_env (it sources constants.sh + git_commit.sh
# for the real binary build, which we don't perform here).
if ! git -C "${REPO_ROOT}" rev-parse HEAD &>/dev/null; then
    git config --global --add safe.directory "${REPO_ROOT}"
fi

# Working directory for staging the stub binaries and zip outputs. Kept
# OUTSIDE the bind-mounted repo so writes don't pierce the host's git tree.
STAGE="$(mktemp -d /tmp/macos-zip-build.XXXXXXXX)"
trap 'rm -rf "${STAGE}"' EXIT

echo "=== Building macOS zips (tag: ${TAG}, stage: ${STAGE}) ==="

# ── Stub Mach-O binaries ─────────────────────────────────────────────
#
# Cross-compile trivial Mach-O binaries so this test runs reproducibly on
# both Linux and macOS dev hosts. The point of this validator is to
# exercise the *packaging* path (sign + zip + GPG-sign), not to package
# the real avalanchego binary — for that, push a tag and let the
# build-macos-release workflow do it on a real macos-14 runner.

mkdir -p "${STAGE}/build"
cat > "${STAGE}/stub.go" <<'EOF'
package main

func main() {}
EOF

echo "Cross-compiling Mach-O stubs (darwin/arm64)..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
    go build -o "${STAGE}/build/avalanchego" "${STAGE}/stub.go"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
    go build -o "${STAGE}/build/subnet-evm"  "${STAGE}/stub.go"

# Confirm the cross-compile actually produced Mach-O (`file` ships in the
# image precisely so this assertion is meaningful).
file "${STAGE}/build/avalanchego" "${STAGE}/build/subnet-evm"

# ── Apple code-signing ──────────────────────────────────────────────
#
# When MACOS_SIGNING_PKCS12_BASE64 is provided, use it (exercises the
# workflow's real-cert path). Otherwise generate a throwaway self-signed
# certificate (proves the workflow's signing step is wired correctly;
# the resulting signature is parseable by rcodesign verify but won't be
# trusted by Apple's notary or Gatekeeper).

APPLE_P12="${STAGE}/macos-signing.p12"
APPLE_P12_PASSWORD=""

if [[ -n "${MACOS_SIGNING_PKCS12_BASE64:-}" ]]; then
    echo "Materializing provided macOS signing P12..."
    printf '%s' "${MACOS_SIGNING_PKCS12_BASE64}" | base64 -d > "${APPLE_P12}"
    APPLE_P12_PASSWORD="${MACOS_SIGNING_PASSWORD:-}"
else
    echo "Generating throwaway self-signed macOS signing certificate..."
    APPLE_P12_PASSWORD="${EPHEMERAL_P12_PASSWORD}"
    rcodesign generate-self-signed-certificate \
        --p12-file "${APPLE_P12}" \
        --p12-password "${APPLE_P12_PASSWORD}" \
        --validity-days 1 \
        --person-name "AvalancheGo Local Test (ephemeral)"
fi

for pkg in avalanchego subnet-evm; do
    echo "Apple-signing ${pkg}..."
    rcodesign sign \
        --p12-file "${APPLE_P12}" \
        --p12-password "${APPLE_P12_PASSWORD}" \
        --code-signature-flags runtime \
        "${STAGE}/build/${pkg}"
done

# ── GPG signing + zip via the production script ─────────────────────
#
# Reuse lib-build-common.sh::setup_gpg to handle ephemeral/provided-key
# branching (same path RPM/DEB packaging uses). The production script
# (build-zip-pkg.sh) reads GPG_KEY_FILE and GPG_PASSPHRASE.

# setup_gpg (called below) materializes the signing key at NFPM_SIGNING_KEY:
# it writes the freshly-generated ephemeral key there, or copies the provided
# key file to that path. Everything downstream reads the key from this path.
INPUT_GPG_KEY_FILE="${GPG_KEY_FILE:-}"
export NFPM_SIGNING_KEY="${STAGE}/gpg-signing-key.asc"
export GPG_PASSPHRASE="${GPG_KEY_PASSPHRASE:-}"

if [[ -z "${INPUT_GPG_KEY_FILE}" ]]; then
    use_ephemeral_gpg_passphrase "GPG_PASSPHRASE"
fi

setup_gpg "${INPUT_GPG_KEY_FILE}" "${OUTPUT_DIR}/GPG-KEY-avalanchego.asc" "MacOS-Zip"

# After setup_gpg, the signing key lives at NFPM_SIGNING_KEY (whether
# ephemeral or copied from the provided key file).
export GPG_KEY_FILE="${NFPM_SIGNING_KEY}"

echo "Invoking production build-zip-pkg.sh in stage dir..."
(
    cd "${STAGE}"
    TAG="${TAG}" \
    GPG_KEY_FILE="${GPG_KEY_FILE}" \
    GPG_PASSPHRASE="${GPG_PASSPHRASE}" \
        bash "${REPO_ROOT}/.github/workflows/build-zip-pkg.sh"
)

# Move zip + sig outputs from stage to the bind-mounted OUTPUT_DIR.
for pkg in avalanchego subnet-evm; do
    cp "${STAGE}/${pkg}-macos-${TAG}.zip"     "${OUTPUT_DIR}/"
    cp "${STAGE}/${pkg}-macos-${TAG}.zip.sig" "${OUTPUT_DIR}/"
done

# ── App Store Connect API key encoding ──────────────────────────────
#
# Exercises the encode-app-store-connect-api-key call the workflow's
# `Notarize zips` step performs. Purely local, no network.
#
# The output JSON contains the private signing key, so we keep it inside
# the ephemeral STAGE (cleaned up by the EXIT trap) and never write it
# to the bind-mounted OUTPUT_DIR. That mirrors the production workflow,
# which mktemps these credentials and removes them via the step's EXIT trap.
# The parse-check happens inline here, while the file is still alive.

APPLE_P8="${STAGE}/notarization.p8"
APPLE_API_KEY_JSON="${STAGE}/api-key.json"
APPLE_KEY_ID=""
APPLE_ISSUER=""

if [[ -n "${MACOS_NOTARIZATION_AUTH_KEY:-}" ]]; then
    echo "Materializing provided notarization API key..."
    printf '%s' "${MACOS_NOTARIZATION_AUTH_KEY}" | base64 -d > "${APPLE_P8}"
    APPLE_KEY_ID="${MACOS_NOTARIZATION_KEY_ID:?MACOS_NOTARIZATION_KEY_ID required when AUTH_KEY provided}"
    APPLE_ISSUER="${MACOS_NOTARIZATION_ISSUER_ID:?MACOS_NOTARIZATION_ISSUER_ID required when AUTH_KEY provided}"
else
    echo "Generating throwaway ECDSA P8 + key id + issuer id..."
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "${APPLE_P8}"
    APPLE_KEY_ID="${EPHEMERAL_NOTARIZATION_KEY_ID}"
    APPLE_ISSUER="${EPHEMERAL_NOTARIZATION_ISSUER}"
fi

echo "Encoding App Store Connect API key..."
rcodesign encode-app-store-connect-api-key \
    --output-path "${APPLE_API_KEY_JSON}" \
    "${APPLE_ISSUER}" "${APPLE_KEY_ID}" "${APPLE_P8}"

# Parse-check the encoded JSON in-place. Field names vary across rcodesign
# versions, so we just assert it parses; the encode step itself succeeding
# is the real signal.
echo "Parse-checking encoded API key JSON..."
jq -e . "${APPLE_API_KEY_JSON}" >/dev/null

# ── Post-conditions ─────────────────────────────────────────────────

assert_files_exist "${OUTPUT_DIR}" \
    "avalanchego-macos-${TAG}.zip" \
    "avalanchego-macos-${TAG}.zip.sig" \
    "subnet-evm-macos-${TAG}.zip" \
    "subnet-evm-macos-${TAG}.zip.sig" \
    "GPG-KEY-avalanchego.asc"

echo "=== macOS zip build complete ==="
ls -la "${OUTPUT_DIR}"
