#!/usr/bin/env bash

set -euo pipefail

: "${DEB_PATH:?DEB_PATH must be set}"

if [[ ! -f "${DEB_PATH}" ]]; then
    echo "Missing DEB package: ${DEB_PATH}" >&2
    exit 1
fi

PACKAGE_SIGNING_GPG_FINGERPRINT="${PACKAGE_SIGNING_GPG_FINGERPRINT:-$(
    gpg --batch --with-colons --list-secret-keys 2>/dev/null \
        | awk -F: '$1 == "fpr" { print $10; exit }'
)}"

: "${PACKAGE_SIGNING_GPG_FINGERPRINT:?PACKAGE_SIGNING_GPG_FINGERPRINT could not be derived}"

dpkg-sig --sign builder -k "${PACKAGE_SIGNING_GPG_FINGERPRINT}" "${DEB_PATH}"
