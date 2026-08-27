#!/usr/bin/env bash

set -euo pipefail

# Build the locked Debian builder image, then build or test a multi-platform
# AvalancheGo runtime image from inside it.

build_only=false
case "${1:-}" in
  "") ;;
  --build-only) build_only=true ;;
  *)
    echo "usage: $0 [--build-only]" >&2
    exit 2
    ;;
esac

avalanchego_path="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-${avalanchego_path}/.cache/bazel-image}"
builder_tag="docker.io/library/avalanchego-bazel-builder:phase1"
registry_image="localhost:5000/avalanchego-bazel:phase3"
case "$(uname -m)" in
  x86_64) builder_architecture=amd64 ;;
  aarch64 | arm64) builder_architecture=arm64 ;;
  *)
    echo "unsupported builder host architecture: $(uname -m)" >&2
    exit 1
    ;;
esac
registry_container_id=""

cleanup() {
  bazelisk shutdown > /dev/null 2>&1 || true

  if [[ -n "$registry_container_id" ]]; then
    docker stop "$registry_container_id" > /dev/null
  fi
}
trap cleanup EXIT

outer_repository_cache="${BAZEL_IMAGE_REPOSITORY_CACHE:-${cache_root}/outer-repository}"
outer_disk_cache="${cache_root}/outer-disk"
inner_repository_cache="${cache_root}/inner-repository"

# The inner Bazel processes use HOME=/cache/home, so they cannot read the
# runner's ~/.bazelrc where setup-bazel configures the remote action cache.
# Pass that configuration through explicitly. Cache compiler outputs, but not
# the large image-layer outputs produced by the final image build.
inner_remote_cache_env=()
inner_remote_cache_args=()
inner_binary_cache_args=()
if [[ -n "${BAZEL_REMOTE_CACHE_URL:-}" && -n "${BAZEL_REMOTE_CACHE_AUTH_HEADER:-}" ]]; then
  inner_remote_cache_env=(
    --env BAZEL_REMOTE_CACHE_URL
    --env BAZEL_REMOTE_CACHE_AUTH_HEADER
  )
  inner_remote_cache_args=(
    "--remote_cache=${BAZEL_REMOTE_CACHE_URL}"
    --remote_timeout=60
    --remote_retries=3
    --remote_cache_compression
    "--remote_header=${BAZEL_REMOTE_CACHE_AUTH_HEADER}"
  )
  inner_binary_cache_args=(--remote_upload_local_results=true)
fi

if [[ ! -S /var/run/docker.sock ]]; then
  echo "Docker socket /var/run/docker.sock is required for the Bazel image load" >&2
  exit 1
fi

docker_socket_gid="$(stat -c '%g' /var/run/docker.sock)"

mkdir -p \
  "$outer_repository_cache" \
  "$outer_disk_cache" \
  "$inner_repository_cache" \
  "${cache_root}/inner-disk-${builder_architecture}-image" \
  "${cache_root}/inner-output-${builder_architecture}-image" \
  "${cache_root}/home"

cd "$avalanchego_path"

# setup-bazel uses this server with test options. Start a new server before the
# image build so Bazel does not discard its analysis cache when options change.
bazelisk shutdown > /dev/null 2>&1 || true

bazelisk run "//bazel/image:load_builder_${builder_architecture}" \
  --repository_cache="$outer_repository_cache" \
  --disk_cache="$outer_disk_cache" \
  --remote_upload_local_results=false

builder_digest="$(docker image inspect --format '{{.Id}}' "$builder_tag")"
git_commit="$(git rev-parse HEAD)"
workspace_status="${cache_root}/workspace_status.sh"
printf '#!/bin/sh\nprintf "STABLE_GIT_COMMIT %s\\n"\n' "$git_commit" > "$workspace_status"
chmod 0755 "$workspace_status"

# Build the image-push target even in build-only mode. Its runfiles provide a
# stable mapping from the image index to each architecture's runtime layer,
# letting the direct binary checks reuse the binaries built by the image target.
image_command=(build //bazel/image:push_avalanchego)
image_arguments=()
if ! "$build_only"; then
  # Host networking makes this disposable registry available as localhost to
  # the inner builder that pushes the image index.
  registry_container_id="$(docker run --rm --detach --network host registry:2)"
  image_command=(run //bazel/image:push_avalanchego)
  image_arguments=(-- --insecure)
fi

# The image index transitions package the amd64 and arm64 manifests in one
# image target. Test mode pushes that target to a disposable local registry.
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --group-add "$docker_socket_gid" \
  --network host \
  --workdir /workspace \
  --env HOME=/cache/home \
  --env USER=avalanchego \
  --env CC=gcc \
  --env IMG_INSECURE=1 \
  --env AVALANCHEGO_BUILDER_IMAGE_DIGEST="$builder_digest" \
  "${inner_remote_cache_env[@]}" \
  --mount "type=bind,src=$avalanchego_path,dst=/workspace,readonly" \
  --mount "type=bind,src=$inner_repository_cache,dst=/cache/repository" \
  --mount "type=bind,src=${cache_root}/inner-disk-${builder_architecture}-image,dst=/cache/disk" \
  --mount "type=bind,src=${cache_root}/inner-output-${builder_architecture}-image,dst=/cache/output" \
  --mount "type=bind,src=${cache_root}/home,dst=/cache/home" \
  --mount "type=bind,src=$workspace_status,dst=/cache/workspace_status.sh,readonly" \
  --mount "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock" \
  "$builder_tag" \
  /usr/local/bin/bazel \
    --batch \
    --output_user_root=/cache/output \
    "${image_command[@]}" \
    --symlink_prefix=/cache/output/bazel- \
    --repository_cache=/cache/repository \
    --disk_cache=/cache/disk \
    "${inner_remote_cache_args[@]}" \
    "${inner_binary_cache_args[@]}" \
    --lockfile_mode=error \
    --config=release \
    --workspace_status_command=/cache/workspace_status.sh \
    --compilation_mode=opt \
    --extra_execution_platforms="//bazel/image:linux_${builder_architecture}" \
    --platforms="//bazel/image:linux_${builder_architecture}" \
    --action_env=CC=gcc \
    --action_env=AVALANCHEGO_BUILDER_IMAGE_DIGEST="$builder_digest" \
    "${image_arguments[@]}" \
  2>&1 | tee "${cache_root}/image-${build_only}.log"

# The image-push runfiles identify the exact layer that each index manifest
# uses. Extract the binary from that layer so the direct checks validate the
# same artifacts the image target packages, without another Bazel invocation.
image_output_root="${cache_root}/inner-output-${builder_architecture}-image"
image_bazel_bin="$(readlink "${image_output_root}/bazel-bin")"
image_bazel_bin="${image_bazel_bin/#\/cache\/output/${image_output_root}}"
image_index="${image_bazel_bin}/bazel/image/avalanchego_index.json"
push_metadata="${image_bazel_bin}/bazel/image/push_avalanchego.json"
push_runfiles="${image_bazel_bin}/bazel/image/push_avalanchego.exe.runfiles"
direct_binary_root="${cache_root}/image-binaries-${builder_architecture}"

for required_file in "$image_index" "$push_metadata" "$push_runfiles"; do
  if [[ ! -e "$required_file" ]]; then
    echo "image build did not produce required output: $required_file" >&2
    exit 1
  fi
done

rm -rf "$direct_binary_root"
mkdir -p "$direct_binary_root"
for architecture in amd64 arm64; do
  manifest_digest="$(jq -r --arg architecture "$architecture" '
    .manifests[]
    | select(.platform.architecture == $architecture)
    | .digest
  ' "$image_index")"
  manifest_index="$(jq -r --arg digest "$manifest_digest" '
    .operations[0].manifests
    | to_entries[]
    | select(.value.descriptor.digest == $digest)
    | .key
  ' "$push_metadata")"
  layer_runfile="${push_runfiles}/++rules_img_private++/_main/bazel/image/push_avalanchego/0/manifests/${manifest_index}/layer/1"
  layer_path="$(readlink "$layer_runfile")"
  layer_path="${layer_path/#\/cache\/output/${image_output_root}}"
  binary_path="${direct_binary_root}/${architecture}/avalanchego"

  if [[ -z "$manifest_digest" || -z "$manifest_index" || ! -f "$layer_path" ]]; then
    echo "failed to find ${architecture} image layer" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$binary_path")"
  tar -xOzf "$layer_path" avalanchego/build/avalanchego > "$binary_path"
  chmod 0755 "$binary_path"

  case "$architecture" in
    amd64) expected_machine="Advanced Micro Devices X86-64" ;;
    arm64) expected_machine="AArch64" ;;
  esac
  echo "inspecting ${architecture} image binary: ${binary_path}"
  if ! readelf --file-header "$binary_path" | grep -Fq "Machine:                           ${expected_machine}"; then
    echo "expected ${architecture} image binary to be ${expected_machine}" >&2
    exit 1
  fi

  if ! "$build_only"; then
    echo "running ${architecture} image binary in fresh Debian 12 slim"
    docker run --rm \
      --platform "linux/${architecture}" \
      --mount "type=bind,src=${binary_path},dst=/avalanchego,readonly" \
      debian:12-slim \
      /avalanchego --version
  fi
done

if "$build_only"; then
  exit 0
fi

docker buildx imagetools inspect "$registry_image"
for architecture in amd64 arm64; do
  docker run --rm \
    --platform "linux/${architecture}" \
    "$registry_image" \
    /avalanchego/build/avalanchego --version
done
