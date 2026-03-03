FROM nixos/nix:latest

# Enable flakes
RUN echo "experimental-features = nix-command flakes" >> /etc/nix/nix.conf

# Pre-build the nix dev shell in a temp location for layer caching
WORKDIR /tmp/nix-cache
COPY flake.nix flake.lock ./
COPY nix/ nix/
RUN nix develop --profile /nix-dev-profile --command true

# Clone the repo from a git bundle containing only the current branch.
# The bundle is created by the initializeCommand in devcontainer.json.
COPY tmp/.git-bundle /tmp/.git-bundle
ARG GIT_REMOTE_URL=https://github.com/ava-labs/avalanchego.git
RUN nix develop --command sh -c "\
    git clone --single-branch /tmp/.git-bundle /workspace/avalanchego && \
    cd /workspace/avalanchego && \
    git remote set-url origin \$GIT_REMOTE_URL" \
    && rm /tmp/.git-bundle

WORKDIR /workspace/avalanchego

# Set the default command to enter the dev shell
CMD ["nix", "develop", "--command", "bash"]
