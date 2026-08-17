#!/usr/bin/env bash
# Runs once after the container is created (postCreateCommand).
#
# Named volumes are created root-owned and are mounted AFTER the image build,
# so their ownership must be fixed at runtime. Chown by username (stable even
# if updateRemoteUserUID remapped the UID).
set -euo pipefail

# The ~/.claude.json symlink is created at build time by the firewood-tools
# feature, not here, so it already exists before Claude Code first writes.

for dir in \
  /usr/local/cargo/registry \
  /usr/local/cargo/git \
  /go/pkg \
  "${HOME}/.cache" \
  "${HOME}/.config/gh" \
  "${HOME}/.claude" \
  "${HOME}/.commandhistory"; do
  sudo mkdir -p "${dir}"
  sudo chown -R vscode:vscode "${dir}"
done

# Verification (mirrors the CI smoke test).
rustup show
go version
nix --version
sccache --show-stats
