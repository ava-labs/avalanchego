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
docker run --detach --privileged --name "$container_name" \
  --volume "$socket_dir:/sockets" \
  --volume "$repo_root:$repo_root" \
  --volume "$nix_store:$nix_store:ro" \
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
  --env "PATH=$PATH" \
  --env "IN_NIX_SHELL=1" \
  --env "HOME=/tmp" \
  --env "TMPDIR=/tmp" \
  --env "AVALANCHEGO_TMPNETCTL=$tmpnetctl" \
  "$container_name" \
  "$(command -v bash)" \
  "$repo_root/scripts/kind-image-cache.sh" produce "$output_dir"
