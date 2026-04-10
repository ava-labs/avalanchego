#!/usr/bin/env bash

set -euo pipefail

LIBRARY_PATH="${1:-${AWS_KMS_PKCS11_LIBRARY_PATH:-${HOME}/.local/lib/pkcs11/aws_kms_pkcs11.so}}"

if [[ -f "${LIBRARY_PATH}" ]]; then
    exit 0
fi

: "${AWS_KMS_PKCS11_DOWNLOAD_URL:?AWS_KMS_PKCS11_DOWNLOAD_URL must be set when aws_kms_pkcs11.so is not already present}"

mkdir -p "$(dirname "${LIBRARY_PATH}")"
curl -fsSL -o "${LIBRARY_PATH}" "${AWS_KMS_PKCS11_DOWNLOAD_URL}"
chmod 0755 "${LIBRARY_PATH}"
