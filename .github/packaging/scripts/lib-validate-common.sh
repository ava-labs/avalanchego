#!/usr/bin/env bash
# Shared functions for package validation scripts.
# Sourced by validate-deb.sh.

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
