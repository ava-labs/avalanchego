#!/usr/bin/env bash
# Verifies that the Kind chaos profile needs no image network pull.
set -euo pipefail

readonly profile_dir="${TEST_SRCDIR}/${TEST_WORKSPACE}/tests/fixture/tmpnet/kind_image_cache_inputs"
readonly host_archive="$profile_dir/host-images.tar"
readonly containerd_archive="$profile_dir/containerd-images.tar"
readonly helm_archive="$profile_dir/helm-cache.tar"
readonly manifest="$profile_dir/manifest"

for required in "$host_archive" "$containerd_archive" "$helm_archive" "$manifest"; do
  [[ -r "$required" ]] || {
    echo "missing Kind chaos cache input: $required" >&2
    echo "Restore the exact kind-chaos cache before invoking this Bazel target." >&2
    exit 1
  }
done

verify_archive_digest() {
  local key="$1"
  local archive="$2"
  local expected
  expected="$(awk -F= -v key="$key" '$1 == key { print $2 }' "$manifest")"
  [[ -n "$expected" ]] || { echo "manifest has no $key" >&2; exit 1; }
  [[ "$expected" == "$(sha256sum "$archive" | awk '{print $1}')" ]] || {
    echo "manifest digest does not match $archive" >&2
    exit 1
  }
}
verify_archive_digest host_images_sha256 "$host_archive"
verify_archive_digest containerd_images_sha256 "$containerd_archive"
verify_archive_digest helm_cache_sha256 "$helm_archive"

# Build before entering the DinD network namespace. The normal tmpnetctl
# launcher rebuilds on every invocation and must not fetch Go modules while
# this test is checking image-cache completeness.
repo_root="$(cd "${TEST_SRCDIR}/${TEST_WORKSPACE}" && pwd -P)"
cd "$repo_root"
./scripts/build_tmpnetctl.sh
readonly tmpnetctl="$repo_root/build/tmpnetctl"

temp_dir="$(mktemp -d)"
socket_dir="$temp_dir/socket"
helm_dir="$temp_dir/helm"
container_name="kind-image-cache-dind-$$"
mkdir -p "$socket_dir" "$helm_dir"

diagnostics() {
  local status=$?
  if (( status != 0 )); then
    echo '=== nested Docker log ===' >&2
    docker logs "$container_name" >&2 || true
    echo '=== nested Docker images ===' >&2
    DOCKER_HOST="unix://$socket_dir/docker.sock" docker image ls --digests >&2 || true
    if DOCKER_HOST="unix://$socket_dir/docker.sock" docker inspect kind-control-plane >/dev/null 2>&1; then
      echo '=== Kind containerd images ===' >&2
      DOCKER_HOST="unix://$socket_dir/docker.sock" docker exec kind-control-plane ctr -n k8s.io images ls >&2 || true
    fi
  fi
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf "$temp_dir"
  exit "$status"
}
trap diagnostics EXIT

# The dind image is a test fixture owned by the outer daemon. The nested
# daemon is the workload's Docker daemon and has no external network.
docker run --detach --privileged --network none --name "$container_name" \
  --volume "$socket_dir:/sockets" \
  docker:dind --host=unix:///sockets/docker.sock --group="$(id -g)" >/dev/null

for _ in $(seq 1 30); do
  if DOCKER_HOST="unix://$socket_dir/docker.sock" docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
DOCKER_HOST="unix://$socket_dir/docker.sock" docker info >/dev/null

# This must fail before a seed is loaded. Use an image unrelated to the
# workload so a correct seed cannot make this check pass accidentally.
if timeout 30 env DOCKER_HOST="unix://$socket_dir/docker.sock" docker pull docker.io/library/hello-world:latest; then
  echo 'the nested Docker daemon unexpectedly reached a registry' >&2
  exit 1
fi

tar --extract --file "$helm_archive" --directory "$helm_dir"

# Kind publishes its API server on loopback in the nested daemon's network
# namespace. nsenter changes only the network namespace: the repository,
# Docker socket, and toolchain remain those of the outer process.
dind_pid="$(docker inspect --format '{{.State.Pid}}' "$container_name")"
sudo --preserve-env=IN_NIX_SHELL,PATH nsenter --target "$dind_pid" --net --preserve-credentials -- env \
  DOCKER_HOST="unix://$socket_dir/docker.sock" \
  HELM_CONFIG_HOME="$helm_dir/config" \
  HELM_CACHE_HOME="$helm_dir/cache" \
  AVALANCHEGO_KIND_OFFLINE=true \
  AVALANCHEGO_KIND_HOST_IMAGES_ARCHIVE="$host_archive" \
  AVALANCHEGO_KIND_CONTAINERD_IMAGES_ARCHIVE="$containerd_archive" \
  AVALANCHEGO_TMPNETCTL="$tmpnetctl" \
  ./scripts/run_task.sh test-robustness
