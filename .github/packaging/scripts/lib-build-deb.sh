#!/usr/bin/env bash

# DEB-specific build functions for build-package.sh.
#
# Sourced (not executed) by build-package.sh when PKG_FORMAT=DEB.
# Provides hooks called at specific points in the build flow.

# Configure gpg-agent for non-interactive dpkg-sig signing.
# Must be called before setup_gpg so the agent is ready.
setup_deb_gpg_agent() {
    local gpg_agent_conf="${HOME}/.gnupg/gpg-agent.conf"
    mkdir -p "$(dirname "${gpg_agent_conf}")"
    if ! grep -q allow-preset-passphrase "${gpg_agent_conf}" 2>/dev/null; then
        echo "allow-preset-passphrase" >> "${gpg_agent_conf}"
        gpgconf --kill gpg-agent 2>/dev/null || true
    fi
}

# Cache GPG passphrase in gpg-agent for dpkg-sig non-interactive signing.
#
# Args: gpg_key_file - path to GPG key (empty = ephemeral, skip caching)
cache_deb_gpg_passphrase() {
    local gpg_key_file="${1:-}"
    if [[ -z "${gpg_key_file}" || -z "${NFPM_DEB_PASSPHRASE:-}" ]]; then
        return
    fi
    local preset_pass
    preset_pass="$(gpgconf --list-dirs libexecdir)/gpg-preset-passphrase"
    local keygrips
    keygrips=$(gpg --batch --with-colons --with-keygrip --list-secret-keys "security@avalabs.org" \
        | awk -F: '$1 == "grp" { print $10 }')
    local kg
    for kg in ${keygrips}; do
        echo "${NFPM_DEB_PASSPHRASE}" | "${preset_pass}" --preset "${kg}"
    done
    echo "GPG passphrase cached in gpg-agent"
}

# Sign a DEB package with dpkg-sig (post-build).
# nfpm's Go openpgp signatures are incompatible with dpkg-sig --verify,
# so we sign after nfpm produces the unsigned .deb.
#
# Args:
#   pkg_path     - full path to the .deb file
#   pkg_filename - filename for display
sign_deb_package() {
    local pkg_path="${1:?package path required}"
    local pkg_filename="${2:?package filename required}"

    local gpg_fingerprint
    gpg_fingerprint=$(gpg --batch --with-colons --list-secret-keys "security@avalabs.org" 2>/dev/null \
        | awk -F: '$1 == "fpr" { print $10; exit }')
    echo "Signing ${pkg_filename} with GPG fingerprint ${gpg_fingerprint}..."
    dpkg-sig --sign builder -k "${gpg_fingerprint}" "${pkg_path}"
}
