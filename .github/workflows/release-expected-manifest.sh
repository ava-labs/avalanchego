#!/usr/bin/env bash
# release-expected-manifest.sh — emit the canonical list of artifact basenames
# expected on the GitHub release for <TAG>.
#
# Usage: release-expected-manifest.sh <TAG>
#
# Each line is one expected basename.
#
# The GPG public key (GPG-KEY-avalanchego) is deliberately NOT listed: the
# key is distributed exclusively via S3. It rides inside the rpms-*
# workflow artifacts for RPM validation, but the manifest-driven publish-set
# assembler ignores any downloaded file that isn't in this manifest, so it
# never reaches the release page.
#
# Deb basenames embed the codename to disambiguate jammy from noble (the deb
# producers emit identically-named files under codename-distinct artifact
# bundles). The release workflow's publish-set assembler resolves each
# codename-suffixed target name by path substring (`*jammy*` or `*noble*`)
# when copying into publish-assets/, preserving the S3 path convention for
# downstream consumers.
#
# Detached signatures (.sig companions) are first-class release deliverables
# and ARE listed in this manifest. PRECONDITION: the linux-binary
# detached-signature PR (PlatCore/5160) and the macOS signing PR
# (PlatCore/5161) MUST be merged on master before this release workflow
# ships. Without them, producers won't upload .sig files and the publish-set
# assembler will fail loudly with "expected artifacts missing" — which is
# exactly the intended fail-closed behavior. Embedded GPG signatures inside
# RPMs and debs do NOT produce separate .sig files and stay off this list.

set -euo pipefail

TAG="${1:?Usage: release-expected-manifest.sh <TAG>}"

cat <<EOF
avalanchego-${TAG}-jammy-amd64.deb
avalanchego-${TAG}-noble-amd64.deb
avalanchego-${TAG}-jammy-arm64.deb
avalanchego-${TAG}-noble-arm64.deb
avalanchego-${TAG}-x86_64.rpm
avalanchego-${TAG}-aarch64.rpm
subnet-evm-${TAG}-x86_64.rpm
subnet-evm-${TAG}-aarch64.rpm
avalanchego-linux-amd64-${TAG}.tar.gz
avalanchego-linux-amd64-${TAG}.tar.gz.sig
avalanchego-linux-arm64-${TAG}.tar.gz
avalanchego-linux-arm64-${TAG}.tar.gz.sig
subnet-evm-linux-amd64-${TAG}.tar.gz
subnet-evm-linux-amd64-${TAG}.tar.gz.sig
subnet-evm-linux-arm64-${TAG}.tar.gz
subnet-evm-linux-arm64-${TAG}.tar.gz.sig
avalanchego-macos-${TAG}.zip
avalanchego-macos-${TAG}.zip.sig
subnet-evm-macos-${TAG}.zip
subnet-evm-macos-${TAG}.zip.sig
EOF
