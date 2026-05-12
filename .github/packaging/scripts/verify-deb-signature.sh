#!/usr/bin/env bash

# Verify a nfpm-signed .deb package using ar + gpg.
#
# nfpm signs DEBs with `method: debsign` + `type: origin`, embedding a
# detached PGP signature as the `_gpgorigin` ar member. The signature
# covers the concatenation of `debian-binary`, `control.tar.*`, and
# `data.tar.*` members. We verify by extracting and re-concatenating.
#
# Tool-independent: requires only `ar` (binutils) and `gpg`, available
# in any base Ubuntu/Debian image. Works identically on jammy and noble.
# Does not depend on dpkg-sig (deprecated, missing from noble) or
# debsig-verify (requires policy XML infrastructure).
#
# The signing key must already be imported into the host's GPG keyring
# before calling this script.
#
# Args:
#   $1 - path to .deb file

set -euo pipefail

DEB_PATH="${1:?deb path required}"

if [[ ! -f "${DEB_PATH}" ]]; then
    echo "ERROR: file not found: ${DEB_PATH}" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

ar x --output="${tmpdir}" "${DEB_PATH}"

sig_member=""
for candidate in _gpgorigin _gpgmaint _gpgbuilder; do
    if [[ -f "${tmpdir}/${candidate}" ]]; then
        sig_member="${tmpdir}/${candidate}"
        break
    fi
done

if [[ -z "${sig_member}" ]]; then
    echo "ERROR: no signature member found in ${DEB_PATH}" >&2
    echo "ar contents:" >&2
    ls -la "${tmpdir}" >&2
    exit 1
fi

# nfpm signs cat(debian-binary, control.tar.*, data.tar.*) in that order.
combined="${tmpdir}/combined"
cat "${tmpdir}/debian-binary" "${tmpdir}"/control.tar.* "${tmpdir}"/data.tar.* > "${combined}"

if ! gpg --verify "${sig_member}" "${combined}" 2>&1; then
    echo "ERROR: GPG signature verification failed for ${DEB_PATH}" >&2
    exit 1
fi

echo "OK: signature verified for ${DEB_PATH}"
