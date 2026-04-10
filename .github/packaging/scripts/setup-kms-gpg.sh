#!/usr/bin/env bash

set -euo pipefail

: "${PACKAGE_SIGNING_KMS_KEY_ID:?PACKAGE_SIGNING_KMS_KEY_ID must be set}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GPG_HOME="${PACKAGE_SIGNING_GPG_HOME:-${GNUPGHOME:-${HOME}/.gnupg}}"
AWS_REGION="${AWS_REGION:-us-east-1}"
AWS_KMS_PKCS11_LIBRARY_PATH="${AWS_KMS_PKCS11_LIBRARY_PATH:-${HOME}/.local/lib/pkcs11/aws_kms_pkcs11.so}"
PACKAGE_SIGNING_LABEL="${PACKAGE_SIGNING_LABEL:-avalanchego-package-signing}"
PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH="${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH:-${PWD}/avalanchego-signing-public.asc}"
AWS_KMS_PKCS11_CONFIG_DIR="${AWS_KMS_PKCS11_CONFIG_DIR:-${HOME}/.config/aws-kms-pkcs11}"
AWS_KMS_PKCS11_CONFIG_PATH="${AWS_KMS_PKCS11_CONFIG_PATH:-${AWS_KMS_PKCS11_CONFIG_DIR}/config.json}"

"${SCRIPT_DIR}/install-aws-kms-pkcs11.sh" "${AWS_KMS_PKCS11_LIBRARY_PATH}"

mkdir -p "${GPG_HOME}" "${AWS_KMS_PKCS11_CONFIG_DIR}" "$(dirname "${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH}")"
chmod 700 "${GPG_HOME}"
export GNUPGHOME="${GPG_HOME}"

CERT_PATH=""
if [[ -n "${PACKAGE_SIGNING_X509_CERT_PEM_BASE64:-}" ]]; then
    CERT_PATH="${AWS_KMS_PKCS11_CONFIG_DIR}/signing-cert.pem"
    printf '%s' "${PACKAGE_SIGNING_X509_CERT_PEM_BASE64}" | base64 --decode > "${CERT_PATH}"
elif [[ -n "${PACKAGE_SIGNING_X509_CERT_PEM:-}" ]]; then
    CERT_PATH="${AWS_KMS_PKCS11_CONFIG_DIR}/signing-cert.pem"
    printf '%s\n' "${PACKAGE_SIGNING_X509_CERT_PEM}" > "${CERT_PATH}"
elif [[ -n "${PACKAGE_SIGNING_X509_CERT_PATH:-}" ]]; then
    CERT_PATH="${PACKAGE_SIGNING_X509_CERT_PATH}"
fi

json_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

cat > "${AWS_KMS_PKCS11_CONFIG_PATH}" <<EOF
{
  "slots": [
    {
      "label": "$(json_escape "${PACKAGE_SIGNING_LABEL}")",
      "kms_key_id": "$(json_escape "${PACKAGE_SIGNING_KMS_KEY_ID}")",
      "aws_region": "$(json_escape "${AWS_REGION}")"$(if [[ -n "${CERT_PATH}" ]]; then printf ',\n      "certificate_path": "%s"' "$(json_escape "${CERT_PATH}")"; fi)
    }
  ]
}
EOF

cat > "${GPG_HOME}/gnupg-pkcs11-scd.conf" <<EOF
providers kms
provider-kms-library ${AWS_KMS_PKCS11_LIBRARY_PATH}
use-gnupg-pin-cache
EOF

if [[ -n "${PACKAGE_SIGNING_OPENPGP_KEY_SHA1:-}" ]]; then
    cat >> "${GPG_HOME}/gnupg-pkcs11-scd.conf" <<EOF
emulate-openpgp
openpgp-sign ${PACKAGE_SIGNING_OPENPGP_KEY_SHA1}
EOF
fi

cat > "${GPG_HOME}/gpg-agent.conf" <<EOF
scdaemon-program $(command -v gnupg-pkcs11-scd)
EOF

gpgconf --kill gpg-agent >/dev/null 2>&1 || true
gpgconf --launch gpg-agent

gpg-connect-agent "SCD LEARN --force" /bye >/dev/null 2>&1 || true
gpg --batch --card-status >/dev/null 2>&1 || true

PACKAGE_SIGNING_GPG_FINGERPRINT="$(
    gpg --batch --with-colons --list-secret-keys 2>/dev/null \
        | awk -F: '$1 == "fpr" { print $10; exit }'
)"

if [[ -z "${PACKAGE_SIGNING_GPG_FINGERPRINT}" ]]; then
    echo "Failed to discover a KMS-backed GPG fingerprint. If gnupg-pkcs11-scd requires OpenPGP emulation for this key, set PACKAGE_SIGNING_OPENPGP_KEY_SHA1 and provide a certificate via PACKAGE_SIGNING_X509_CERT_*." >&2
    exit 1
fi

gpg --batch --yes --armor --export "${PACKAGE_SIGNING_GPG_FINGERPRINT}" > "${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH}"

if [[ -n "${GITHUB_ENV:-}" ]]; then
    {
        printf 'PACKAGE_SIGNING_GPG_FINGERPRINT=%s\n' "${PACKAGE_SIGNING_GPG_FINGERPRINT}"
        printf 'PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH=%s\n' "${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH}"
    } >> "${GITHUB_ENV}"
fi

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
        printf 'fingerprint=%s\n' "${PACKAGE_SIGNING_GPG_FINGERPRINT}"
        printf 'public_key_path=%s\n' "${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH}"
    } >> "${GITHUB_OUTPUT}"
fi

echo "Configured KMS-backed GPG fingerprint: ${PACKAGE_SIGNING_GPG_FINGERPRINT}"
echo "Exported public key to: ${PACKAGE_SIGNING_PUBLIC_KEY_OUTPUT_PATH}"
