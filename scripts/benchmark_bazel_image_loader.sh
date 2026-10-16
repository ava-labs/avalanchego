#!/usr/bin/env bash

# Benchmark rules_img Docker and direct-containerd loads in disposable Docker-in-
# Docker daemons. This does not alter the host Docker daemon.
set -euo pipefail

iterations="${1:-3}"
if ! [[ "$iterations" =~ ^[1-9][0-9]*$ ]]; then
  echo "usage: $0 [iterations]" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -m)" in
  x86_64) builder_architecture=amd64 ;;
  aarch64 | arm64) builder_architecture=arm64 ;;
  *)
    echo "unsupported builder host architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

# Pin the benchmark daemon independently from the host Docker installation.
dind_image="${DIND_IMAGE:-docker:29.6.0-dind}"
target="//bazel/image:load_builder_${builder_architecture}"
load_daemon_setting="@rules_img//img/settings:load_daemon"
output_base="$(cd "$repo_root" && bazelisk info output_base)"

cleanup() {
  if [[ -n "${daemon_name:-}" ]]; then
    docker rm --force "$daemon_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

loader_for() {
  local daemon=$1
  local loader

  (
    cd "$repo_root"
    bazelisk build "$target" "--${load_daemon_setting}=${daemon}" >/dev/null
    loader="$(bazelisk cquery --output=files "$target" "--${load_daemon_setting}=${daemon}")"
    realpath "$repo_root/$loader"
  )
}

start_daemon() {
  local mode=$1

  daemon_name="bazel-image-loader-benchmark-${mode}-$$"
  local -a arguments=(
    --detach
    --privileged
    --name "$daemon_name"
    --volume "$repo_root:$repo_root:ro"
    --volume "$output_base:$output_base:ro"
  )
  local -a daemon_arguments=(--feature containerd-snapshotter=false)
  if [[ "$mode" == "containerd" ]]; then
    daemon_arguments=(--feature containerd-snapshotter=true)
  fi

  docker run "${arguments[@]}" "$dind_image" "${daemon_arguments[@]}" >/dev/null
  until docker exec "$daemon_name" docker info >/dev/null 2>&1; do
    sleep 0.1
  done

  # The loader can reuse the pinned Debian base layer. Keep this network fetch
  # outside the timed section so it does not hide the import cost.
  docker exec "$daemon_name" docker pull debian:12-slim >/dev/null
}

benchmark_mode() {
  local mode=$1
  local loader
  local iteration elapsed_start elapsed_end elapsed_ms
  local -a loader_user=()

  loader="$(loader_for "$mode")"
  echo "${mode} loader:"
  for ((iteration = 1; iteration <= iterations; iteration++)); do
    start_daemon "$mode"
    if [[ "$mode" == "docker" ]]; then
      # Match ordinary Docker access: the Docker API is available, but Docker's
      # private containerd directory remains inaccessible.
      docker exec "$daemon_name" chmod 0666 /var/run/docker.sock
      loader_user=(--user 65534:65534)
    fi
    elapsed_start="$(date +%s%N)"
    docker exec "${loader_user[@]}" "$daemon_name" "$loader" >/dev/null 2>&1
    elapsed_end="$(date +%s%N)"
    elapsed_ms=$(((elapsed_end - elapsed_start) / 1000000))
    docker exec "$daemon_name" docker image inspect docker.io/library/avalanchego-bazel-builder:phase1 >/dev/null
    printf '  run %d: %d ms\n' "$iteration" "$elapsed_ms"
    docker rm --force "$daemon_name" >/dev/null
    daemon_name=""
  done
}

printf 'Target: %s\nDIND image: %s\nIterations: %s\n\n' "$target" "$dind_image" "$iterations"
benchmark_mode docker
benchmark_mode containerd
