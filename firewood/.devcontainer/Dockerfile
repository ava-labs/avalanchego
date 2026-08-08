# Firewood Development Container
# Ubuntu base with Rust, Go, Nix, and development tools

ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION}

# Build arguments for customization
ARG USERNAME=developer
ARG USER_UID=1000
ARG USER_GID=${USER_UID}
ARG GO_VERSION=1.25.8
ARG RUST_STABLE_VERSION=stable
ARG RUST_NIGHTLY_VERSION=nightly

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
  build-essential \
  ca-certificates \
  clang \
  cmake \
  curl \
  git \
  openssh-client \
  gnupg \
  jq \
  libssl-dev \
  lsb-release \
  pkg-config \
  shellcheck \
  sudo \
  unzip \
  wget \
  xz-utils \
  && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN mkdir -p -m 755 /etc/apt/keyrings \
  && wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null \
  && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | tee /etc/apt/sources.list.d/github-cli.list >/dev/null \
  && apt-get update \
  && apt-get install -y gh \
  && rm -rf /var/lib/apt/lists/*

# Delete the built-in ubuntu user and create non-root user with sudo access
RUN userdel --force --remove ubuntu || true \
  && groupadd --gid ${USER_GID} ${USERNAME} \
  && useradd --uid ${USER_UID} --gid ${USER_GID} -m -s /bin/bash ${USERNAME} \
  && echo "${USERNAME} ALL=(root) NOPASSWD:ALL" >/etc/sudoers.d/devcontainer \
  && chmod 0440 /etc/sudoers.d/devcontainer

# Install Go
RUN ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/') \
  && wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -O /tmp/go.tar.gz \
  && tar -C /usr/local -xzf /tmp/go.tar.gz \
  && rm /tmp/go.tar.gz

# Set up Go environment
ENV GOROOT=/usr/local/go
ENV PATH="${GOROOT}/bin:${PATH}"

# Install Nix using Determinate Systems installer (as root, for multi-user mode)
RUN curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix \
  | sh -s -- install linux --no-confirm --init none \
  && echo "trusted-users = root ${USERNAME}" >>/etc/nix/nix.conf

# Add user to nix trusted users and nixbld group
RUN usermod -aG nixbld ${USERNAME} || true

# Switch to non-root user for remaining installations
USER ${USERNAME}
WORKDIR /home/${USERNAME}

# Set up user environment variables
ENV HOME=/home/${USERNAME}
ENV CARGO_HOME="${HOME}/.cargo"
ENV RUSTUP_HOME="${HOME}/.rustup"
ENV GOPATH="${HOME}/go"
ENV SCCACHE_DIR="${HOME}/.cache/sccache"
ENV PATH="${CARGO_HOME}/bin:${GOPATH}/bin:${PATH}"

# Install Rust via rustup
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
  | sh -s -- -y --default-toolchain ${RUST_STABLE_VERSION} \
  && . "${CARGO_HOME}/env" \
  && rustup toolchain install ${RUST_NIGHTLY_VERSION} \
  && rustup component add clippy rustfmt rust-src rust-docs llvm-tools --toolchain ${RUST_STABLE_VERSION} \
  && rustup component add clippy rustfmt rust-src rust-docs llvm-tools --toolchain ${RUST_NIGHTLY_VERSION}

# Install cargo-binstall first to download pre-built binaries for some heavy
# dependencies (like sccache).
RUN . "${CARGO_HOME}/env" \
  && curl -L --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/cargo-bins/cargo-binstall/main/install-from-binstall-release.sh | bash

# Install sccache next (needed for subsequent cargo installs, in case they need
# to compile from source)
RUN . "${CARGO_HOME}/env" && cargo binstall --locked sccache

# Configure Cargo to use sccache globally
RUN mkdir -p "${CARGO_HOME}" \
  && printf '%s\n' \
    '[build]' \
    "rustc-wrapper = \"${CARGO_HOME}/bin/sccache\"" \
    '' \
    '[env]' \
    "RUSTC_WRAPPER = \"${CARGO_HOME}/bin/sccache\"" \
    'CMAKE_C_COMPILER_LAUNCHER = "sccache"' \
    'CMAKE_CXX_COMPILER_LAUNCHER = "sccache"' \
    >"${CARGO_HOME}/config.toml"

# Install Cargo tools (using sccache now)
RUN . "${CARGO_HOME}/env" \
  && cargo binstall --locked \
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
    starship \
  && rm -rf "${CARGO_HOME}/registry" "${CARGO_HOME}/git" "${SCCACHE_DIR}" \
  && mkdir -p "${CARGO_HOME}/registry" "${CARGO_HOME}/git" "${SCCACHE_DIR}"

# Install Go tools
RUN curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin \
  && go install github.com/reteps/dockerfmt@latest \
  && go install mvdan.cc/sh/v3/cmd/shfmt@latest \
  && go clean -cache -testcache -modcache -fuzzcache

# Install Claude Code CLI
RUN curl -fsSL https://claude.ai/install.sh | bash

# Configure starship prompt
RUN echo 'eval "$(starship init bash)"' >>"${HOME}/.bashrc"

# Configure Nix for the user
RUN mkdir -p "${HOME}/.config/nix" \
  && echo "experimental-features = nix-command flakes" >"${HOME}/.config/nix/nix.conf"

# Source nix in shell profiles
RUN echo '. /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' >>"${HOME}/.bashrc" \
  && echo '. /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' >>"${HOME}/.profile"

# Set working directory to workspace
WORKDIR /workspaces/firewood

# Default command
CMD ["bash"]
