#!/usr/bin/env bash
# Produce a Kind image-cache profile from a fresh, disposable Docker daemon.
set -euo pipefail

[[ $# == 1 ]] || { echo "usage: $0 <output-directory>" >&2; exit 2; }
readonly output_dir="$1"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
temp_dir="$(mktemp -d)"
readonly temp_dir
readonly socket_dir="$temp_dir/socket"
readonly container_name="kind-image-cache-producer-$$"
readonly nix_store="/nix"
[[ -d "$nix_store" ]] || { echo "Nix store is required: $nix_store" >&2; exit 1; }
git_dir="$(git rev-parse --absolute-git-dir)"
readonly git_dir
git_common_dir="$(git rev-parse --path-format=absolute --git-common-dir)"
readonly git_common_dir
mkdir -p "$socket_dir"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf "$temp_dir"
}
trap cleanup EXIT

# Build before entering the DinD network namespace. bin/tmpnetctl normally
# rebuilds on every invocation, which would turn Go module resolution into an
# accidental dependency of the image-cache producer.
"$repo_root/scripts/build_tmpnetctl.sh"
readonly tmpnetctl="$repo_root/build/tmpnetctl"

# This daemon has ordinary network access only while discovering the workload
# delta. It has no images when it starts.
docker_args=(
  --detach
  --privileged
  --name "$container_name"
  --volume "$socket_dir:/sockets"
  --volume "$repo_root:$repo_root"
  --volume "$nix_store:$nix_store:ro"
)
# A linked worktree's .git file points outside the checkout. Mount its gitdir
# and common directory so Task can resolve the repository root in DinD too.
if [[ -f "$repo_root/.git" ]]; then
  docker_args+=(
    --volume "$git_dir:$git_dir"
    --volume "$git_common_dir:$git_common_dir:ro"
  )
fi
docker run "${docker_args[@]}" \
  docker:dind --host=unix:///sockets/docker.sock --group="$(id -g)" >/dev/null
for _ in $(seq 1 30); do
  if DOCKER_HOST="unix://$socket_dir/docker.sock" docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
DOCKER_HOST="unix://$socket_dir/docker.sock" docker info >/dev/null

# Run the real command in the DinD container rather than nsentering only its
# network namespace. Its resolver then reaches the online DinD network while
# the mounted repository and Nix store provide the same command and tools as
# the outer producer.
docker exec \
  --workdir "$repo_root" \
  --env "DOCKER_HOST=unix:///sockets/docker.sock" \
  --env "PATH=$PATH:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  --env "IN_NIX_SHELL=1" \
  --env "HOME=/tmp" \
  --env "TMPDIR=/tmp" \
  --env "GIT_CONFIG_COUNT=1" \
  --env "GIT_CONFIG_KEY_0=safe.directory" \
  --env "GIT_CONFIG_VALUE_0=$repo_root" \
  --env "AVALANCHEGO_TMPNETCTL=$tmpnetctl" \
  "$container_name" \
  "$(command -v bash)" \
  "$repo_root/scripts/kind-image-cache.sh" produce "$output_dir"

# The nested daemon writes the bind-mounted archive as root. Make the exact
# files consumable by the outer runner's cache and artifact actions.
docker exec --workdir "$repo_root" "$container_name" chmod -R a+rX "$output_dir"
