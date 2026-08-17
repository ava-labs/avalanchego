#!/usr/bin/env bash
# Firewood developer tooling.
#
# Installs the Rust/Go tooling that has no maintained upstream devcontainer
# feature. Runs as root at image-build time, AFTER the rust and go features
# (see installsAfter in devcontainer-feature.json).
set -euo pipefail

# Toolchain homes are provided by the rust/go features. Re-export them here
# because a feature install script does not run as a login shell.
export RUSTUP_HOME="${RUSTUP_HOME:-/usr/local/rustup}"
export CARGO_HOME="${CARGO_HOME:-/usr/local/cargo}"
export GOROOT="${GOROOT:-/usr/local/go}"
export GOPATH="${GOPATH:-/go}"
export PATH="${CARGO_HOME}/bin:${GOROOT}/bin:${GOPATH}/bin:${PATH}"

# The devcontainer runtime passes the target user in _REMOTE_USER(_HOME).
USERNAME="${_REMOTE_USER:-vscode}"
USER_HOME="${_REMOTE_USER_HOME:-/home/${USERNAME}}"

# 1. Build dependencies and shell tooling not present in the base image.
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  build-essential \
  clang \
  cmake \
  jq \
  libssl-dev \
  pkg-config \
  shellcheck
rm -rf /var/lib/apt/lists/*

# 2. Extra components on stable (beyond the rust feature's `default` profile)
#    and a full nightly toolchain.
rustup component add llvm-tools rust-docs
rustup toolchain install nightly --profile minimal
rustup component add clippy rustfmt rust-src rust-docs llvm-tools --toolchain nightly

# 3. cargo-binstall (prebuilt) so the remaining tools install without compiling.
curl -L --proto '=https' --tlsv1.2 -sSf \
  https://raw.githubusercontent.com/cargo-bins/cargo-binstall/main/install-from-binstall-release.sh | bash

# 4. sccache, then wire it in as the global rustc wrapper.
cargo binstall --no-confirm --locked sccache
cat > "${CARGO_HOME}/config.toml" << EOF
[build]
rustc-wrapper = "${CARGO_HOME}/bin/sccache"

[env]
RUSTC_WRAPPER = "${CARGO_HOME}/bin/sccache"
CMAKE_C_COMPILER_LAUNCHER = "sccache"
CMAKE_CXX_COMPILER_LAUNCHER = "sccache"
EOF

# 5. Cargo developer tools.
cargo binstall --no-confirm --locked \
  ast-grep \
  cargo-edit \
  cargo-expand \
  cargo-machete \
  cargo-msrv \
  cargo-nextest \
  git-cliff \
  just \
  ripgrep \
  rustfilt \
  starship

# 6. Go developer tools, then drop the build-time Go caches.
go install github.com/reteps/dockerfmt@latest
go install mvdan.cc/sh/v3/cmd/shfmt@latest
go clean -cache -testcache -modcache -fuzzcache

# 7. Shell init for the developer user.
cat >> "${USER_HOME}/.bashrc" << EOF

# Firewood devcontainer
eval "\$(starship init bash)"
export HISTFILE=${USER_HOME}/.commandhistory/.bash_history
export PROMPT_COMMAND='history -a'
EOF

# 8. Redirect ~/.claude.json into the ~/.claude volume so settings persist
#    across rebuilds. The symlink lives at the home root (outside any volume),
#    so it stays in the image and points into the volume.
#    See anthropics/devcontainer-features#24.
mkdir -p "${USER_HOME}/.claude" "${USER_HOME}/.commandhistory"
ln -snf "${USER_HOME}/.claude/user-claude.json" "${USER_HOME}/.claude.json"

# 9. Empty the build-time cargo caches so the mounted volumes start clean.
rm -rf "${CARGO_HOME}/registry" "${CARGO_HOME}/git"
mkdir -p "${CARGO_HOME}/registry" "${CARGO_HOME}/git"

# 10. Ownership for the developer user.
chown -R "${USERNAME}:${USERNAME}" \
  "${USER_HOME}/.bashrc" \
  "${USER_HOME}/.claude" \
  "${USER_HOME}/.claude.json" \
  "${USER_HOME}/.commandhistory"
