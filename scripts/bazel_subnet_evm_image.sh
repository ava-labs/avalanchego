#!/usr/bin/env bash

set -euo pipefail

# Build the locked Debian builder image, then build or test a multi-platform
# Subnet-EVM image from inside it.

build_only=false
case "${1:-}" in
  "") ;;
  --build-only) build_only=true ;;
  *)
    echo "usage: $0 [--build-only]" >&2
    exit 2
    ;;
esac

workspace_path="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-${workspace_path}/.cache/bazel-subnet-evm-image}"
builder_tag="docker.io/library/avalanchego-bazel-builder:phase1"
registry_image="localhost:5000/subnet-evm-bazel:phase3"

# CI restores these digest-pinned Docker inputs from an Actions cache. Keep
# local tags distinct by platform because Docker's classic image store cannot
# retain both platforms under one tag.
docker_cache_archive="${BAZEL_IMAGE_DOCKER_CACHE_ARCHIVE:-}"
write_docker_cache="${BAZEL_IMAGE_WRITE_DOCKER_CACHE:-false}"
registry_base_image="docker.io/library/registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
registry_base_image_tag="avalanchego-ci/registry:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
debian_base_image_amd64="docker.io/library/debian@sha256:5ae3c39ebd15e229dcedd5cee596b2497182493d41ff162e824ba13fc1b2b867"
debian_base_image_arm64="docker.io/library/debian@sha256:6bd27d44e6c32a66bbd72d7cb2b76a8ae3497ec2e5274a81abd1b37f6013fa1f"
debian_base_image_amd64_tag="avalanchego-ci/debian-12-slim:5ae3c39ebd15e229dcedd5cee596b2497182493d41ff162e824ba13fc1b2b867-amd64"
debian_base_image_arm64_tag="avalanchego-ci/debian-12-slim:6bd27d44e6c32a66bbd72d7cb2b76a8ae3497ec2e5274a81abd1b37f6013fa1f-arm64"
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

if [[ -n "$docker_cache_archive" && -f "$docker_cache_archive" ]]; then
  docker load --input "$docker_cache_archive"
fi

ensure_docker_image() {
  local source_image="$1"
  local platform="$2"
  local local_image="$3"
  local expected_architecture
  local image_architecture
  local image_id

  if docker image inspect "$local_image" > /dev/null 2>&1; then
    return
  fi

  expected_architecture="${platform#linux/}"
  if image_id="$(docker image inspect --format '{{.Id}} {{.Architecture}}' "$source_image" 2> /dev/null)"; then
    read -r image_id image_architecture <<<"$image_id"
    if [[ "$image_architecture" == "$expected_architecture" ]]; then
      docker tag "$image_id" "$local_image"
      return
    fi
  fi

  docker pull --platform "$platform" "$source_image"
  image_id="$(docker image inspect --format '{{.Id}}' "$source_image")"
  docker tag "$image_id" "$local_image"
}

# Build-only local validation does not need Docker base images. CI populates its
# archive in build-only mode too, so a later test-mode job can restore all
# Docker inputs without a registry pull.
if ! "$build_only" || [[ -n "$docker_cache_archive" ]]; then
  ensure_docker_image "$registry_base_image" "linux/${builder_architecture}" "$registry_base_image_tag"
  ensure_docker_image "$debian_base_image_amd64" linux/amd64 "$debian_base_image_amd64_tag"
  ensure_docker_image "$debian_base_image_arm64" linux/arm64 "$debian_base_image_arm64_tag"
fi

docker_socket_gid="$(stat -c '%g' /var/run/docker.sock)"

mkdir -p \
  "$outer_repository_cache" \
  "$outer_disk_cache" \
  "$inner_repository_cache" \
  "${cache_root}/inner-disk-${builder_architecture}-image" \
  "${cache_root}/inner-output-${builder_architecture}-image" \
  "${cache_root}/home"

cd "$workspace_path"

# setup-bazel uses this server with test options. Start a new server before the
# image build so Bazel does not discard its analysis cache when options change.
bazelisk shutdown > /dev/null 2>&1 || true

if ! docker image inspect "$builder_tag" > /dev/null 2>&1; then
  bazelisk run "//bazel/image:load_builder_${builder_architecture}" \
    --repository_cache="$outer_repository_cache" \
    --disk_cache="$outer_disk_cache" \
    --remote_upload_local_results=false
fi

if [[ "$write_docker_cache" == true && -n "$docker_cache_archive" ]]; then
  mkdir -p "$(dirname "$docker_cache_archive")"
  temporary_docker_cache_archive="${docker_cache_archive}.tmp"
  docker image save \
    --output "$temporary_docker_cache_archive" \
    "$builder_tag" \
    "$registry_base_image_tag" \
    "$debian_base_image_amd64_tag" \
    "$debian_base_image_arm64_tag"
  mv "$temporary_docker_cache_archive" "$docker_cache_archive"
fi

builder_digest="$(docker image inspect --format '{{.Id}}' "$builder_tag")"
git_commit="$(git rev-parse HEAD)"
workspace_status="${cache_root}/workspace_status.sh"
printf '#!/bin/sh\nprintf "STABLE_GIT_COMMIT %s\\n"\n' "$git_commit" > "$workspace_status"
chmod 0755 "$workspace_status"

# Build the image-push target even in build-only mode. Its runfiles provide a
# stable mapping from the image index to each architecture's plugin layer,
# letting the direct binary checks reuse the plugins built by the image target.
image_command=(build //bazel/image:push_subnet_evm)
image_arguments=()
if ! "$build_only"; then
  # Host networking makes this disposable registry available as localhost to
  # the inner builder that pushes the image index.
  registry_container_id="$(docker run --rm --detach --network host "$registry_base_image_tag")"
  image_command=(run //bazel/image:push_subnet_evm)
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
  --mount "type=bind,src=$workspace_path,dst=/workspace,readonly" \
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
image_index="${image_bazel_bin}/bazel/image/subnet_evm_index.json"
push_metadata="${image_bazel_bin}/bazel/image/push_subnet_evm.json"
push_runfiles="${image_bazel_bin}/bazel/image/push_subnet_evm.exe.runfiles"
direct_binary_root="${cache_root}/subnet-evm-image-binaries-${builder_architecture}"

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
  layer_runfile="${push_runfiles}/++rules_img_private++/_main/bazel/image/push_subnet_evm/0/manifests/${manifest_index}/layer/2"
  layer_path="$(readlink "$layer_runfile")"
  layer_path="${layer_path/#\/cache\/output/${image_output_root}}"
  binary_path="${direct_binary_root}/${architecture}/subnet-evm"

  if [[ -z "$manifest_digest" || -z "$manifest_index" || ! -f "$layer_path" ]]; then
    echo "failed to find ${architecture} image layer" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$binary_path")"
  tar -xOzf "$layer_path" avalanchego/build/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy > "$binary_path"
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
    case "$architecture" in
      amd64) debian_smoke_image="$debian_base_image_amd64_tag" ;;
      arm64) debian_smoke_image="$debian_base_image_arm64_tag" ;;
    esac
    docker run --rm \
      --platform "linux/${architecture}" \
      --mount "type=bind,src=${binary_path},dst=/plugin,readonly" \
      "$debian_smoke_image" \
      /plugin --version
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
    /avalanchego/build/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy --version
done
