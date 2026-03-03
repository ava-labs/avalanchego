FROM nixos/nix:2.34.0

# Enable flakes
RUN echo "experimental-features = nix-command flakes" >> /etc/nix/nix.conf

# Pre-build the nix dev shell in a temp location for layer caching
WORKDIR /tmp/nix-cache
COPY flake.nix flake.lock ./
COPY nix/ nix/
RUN nix develop --profile /nix-dev-profile --command true

# Clone the repo from a git bundle containing HEAD (and the branch ref if available).
# The bundle and remote config are created by the initializeCommand in
# devcontainer.json.
COPY tmp/.git-bundle /tmp/.git-bundle
COPY tmp/.git-remotes /tmp/.git-remotes
RUN nix develop --command sh -c "\
    git clone /tmp/.git-bundle /workspace/avalanchego && \
    cd /workspace/avalanchego && \
    git remote remove origin && \
    # IFS='=' with two variables splits on the first '=' only, so values containing '=' (e.g. URLs) are preserved.
    while IFS='=' read -r key value; do git config \"\$key\" \"\$value\"; done < /tmp/.git-remotes" \
    && rm /tmp/.git-bundle /tmp/.git-remotes

WORKDIR /workspace/avalanchego

# Set the default command to enter the dev shell
CMD ["nix", "develop", "--command", "bash"]
