#!/usr/bin/env bash
# Shared functions for package validation scripts.
# Sourced by validate-rpm.sh and validate-deb.sh.

# Detect host architecture for the given package format.
# Sets PACKAGE_ARCH (global) if not already set by the caller.
#
# Args: format ("RPM" or "DEB")
detect_host_arch() {
    local format="${1:?format required}"
    if [[ -n "${PACKAGE_ARCH:-}" ]]; then
        return  # already set by caller
    fi
    local arch
    arch=$(uname -m)
    case "${format}" in
        RPM)
            case "${arch}" in
                x86_64) PACKAGE_ARCH="x86_64" ;;
                arm64)  PACKAGE_ARCH="aarch64" ;;
                *)      PACKAGE_ARCH="${arch}" ;;
            esac
            ;;
        DEB)
            case "${arch}" in
                x86_64)        PACKAGE_ARCH="amd64" ;;
                aarch64|arm64) PACKAGE_ARCH="arm64" ;;
                *)             PACKAGE_ARCH="${arch}" ;;
            esac
            ;;
    esac
}

# Verify that all expected package files exist in a directory.
#
# Args: pkg_dir file1 [file2 ...]
assert_files_exist() {
    local pkg_dir="${1:?package directory required}"; shift
    for f in "$@"; do
        if [[ ! -f "${pkg_dir}/${f}" ]]; then
            echo "ERROR: expected file not found: ${pkg_dir}/${f}" >&2
            exit 1
        fi
    done
}
