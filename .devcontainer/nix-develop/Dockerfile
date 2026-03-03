FROM nixos/nix:2.34.0

# Which flake devShell to use (e.g. "default", "ai")
ARG NIX_SHELL=default

# Enable flakes
RUN echo "experimental-features = nix-command flakes" >> /etc/nix/nix.conf

# Pre-build the nix dev shell in a temp location for layer caching
WORKDIR /tmp/nix-cache
COPY flake.nix flake.lock ./
COPY nix/ nix/
RUN nix develop ".#${NIX_SHELL}" --profile /nix-dev-profile --command true

# Create non-root dev user (nixos/nix has no adduser; edit files directly)
RUN echo 'dev:x:1000:1000::/home/dev:/bin/sh' >> /etc/passwd \
    && echo 'dev:x:1000:dev' >> /etc/group \
    && echo 'dev:!:1::::::' >> /etc/shadow \
    && mkdir -p /home/dev \
    && chown 1000:1000 /home/dev

# Clone the repo from a git bundle containing HEAD (and the branch ref if available).
# The bundle and remote config are created by the initializeCommand in
# devcontainer.json.
COPY tmp/.git-bundle /tmp/.git-bundle
COPY tmp/.git-remotes /tmp/.git-remotes
RUN nix develop /nix-dev-profile --command sh -c "\
    git clone /tmp/.git-bundle /workspace/avalanchego && \
    cd /workspace/avalanchego && \
    # Replace the cloned commit with an orphan commit (same tree, no parents).
    # This prevents nix's libgit2 from failing when the bundle comes from a
    # shallow clone whose commit references missing parent objects.
    TREE=\$(git rev-parse HEAD^{tree}) && \
    NEW_COMMIT=\$(GIT_AUTHOR_NAME=build GIT_AUTHOR_EMAIL=build@localhost GIT_COMMITTER_NAME=build GIT_COMMITTER_EMAIL=build@localhost git commit-tree \"\$TREE\" -m 'devcontainer init') && \
    git update-ref HEAD \"\$NEW_COMMIT\" && \
    rm -f .git/shallow && \
    git remote remove origin && \
    # IFS='=' with two variables splits on the first '=' only, so values containing '=' (e.g. URLs) are preserved.
    while IFS='=' read -r key value; do git config \"\$key\" \"\$value\"; done < /tmp/.git-remotes" \
    && rm /tmp/.git-bundle /tmp/.git-remotes

# Transfer ownership after all root-level nix operations are complete
RUN chown -R dev:dev /nix /workspace

WORKDIR /workspace/avalanchego

# Persist the shell selection for runtime (used by build_devcontainer.sh --run)
ENV NIX_SHELL=${NIX_SHELL}

USER dev

# Set the default command to enter the dev shell
CMD nix develop ".#${NIX_SHELL}" --command bash
