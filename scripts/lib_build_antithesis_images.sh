#!/usr/bin/env bash

set -euo pipefail

# This script defines helper functions to enable building images for antithesis test setups. It is
# intended to be reusable by repos other than avalanchego so almost all inputs are accepted as parameters
# rather than being discovered from the environment.
#
# Since this file only defines functions, it is intended to be sourced rather than executed.

# Build the image that enables compiling golang binaries for the node and workload image
# builds. The builder image is intended to enable building instrumented binaries if built
# on amd64 and non-instrumented binaries if built on arm64.
function build_antithesis_builder_image {
  local go_version=$1
  local image_name=$2
  local avalanchego_path=$3
  local target_path=$4

  local base_dockerfile="${avalanchego_path}/tests/antithesis/Dockerfile"
  local builder_dockerfile="${base_dockerfile}.builder-instrumented"
  if [[ "$(go env GOARCH)" == "arm64" ]]; then
    # Antithesis instrumentation is only supported on amd64. On apple silicon (arm64),
    # an uninstrumented Dockerfile will be used to enable local test development.
    builder_dockerfile="${base_dockerfile}.builder-uninstrumented"
  fi

  docker buildx build --build-arg GO_VERSION="${go_version}" -t "${image_name}" -f "${builder_dockerfile}" "${target_path}"
}

# Build the antithesis node, workload, and config images.
function build_antithesis_images {
  local go_version=$1
  local image_prefix=$2
  local base_image_name=$3
  local image_tag=$4
  local node_image_tag=$5
  local base_dockerfile=$6
  local uninstrumented_node_dockerfile=$7
  local target_path=$8
  local node_only=${9:-}
  local avalanchego_commit=${10:-}
  local uninstrumented_node_target=${11:-}

  # Define image names
  if [[ -n "${image_prefix}" ]]; then
    base_image_name="${image_prefix}/${base_image_name}"
  fi
  local node_image_name="${base_image_name}-node:${image_tag}"
  local workload_image_name="${base_image_name}-workload:${image_tag}"
  local config_image_name="${base_image_name}-config:${image_tag}"

  # Define dockerfiles
  local node_dockerfile="${base_dockerfile}.node"
  # Working directory for instrumented builds
  local builder_workdir="/instrumented/customer"
  if [[ "$(go env GOARCH)" == "arm64" ]]; then
    # Antithesis instrumentation is only supported on amd64. On apple silicon (arm64),
    # uninstrumented Dockerfiles will be used to enable local test development.
    node_dockerfile="${uninstrumented_node_dockerfile}"
    # Working directory for uninstrumented builds
    builder_workdir="/build"
  fi

  # Define default build command
  local docker_cmd="docker buildx build\
 --build-arg GO_VERSION=${go_version}\
 --build-arg BUILDER_IMAGE_TAG=${image_tag}\
 --build-arg BUILDER_WORKDIR=${builder_workdir}"
  if [[ -n "${avalanchego_commit}" ]]; then
    docker_cmd="${docker_cmd} --build-arg AVALANCHEGO_COMMIT=${avalanchego_commit}"
  fi

  # By default the node image is intended to be local-only.
  AVALANCHEGO_NODE_IMAGE="antithesis-avalanchego-node:${node_image_tag}"

  if [[ -n "${image_prefix}" && -z "${node_only}" ]]; then
    # Push images with an image prefix since the prefix defines a registry location, and only if building
    # all images. When building just the node image the image is only intended to be used locally.
    docker_cmd="${docker_cmd} --push"

    # When the node image is pushed as part of the build, references to the image must be qualified.
    AVALANCHEGO_NODE_IMAGE="${image_prefix}/${AVALANCHEGO_NODE_IMAGE}"
  fi

  # Ensure the correct node image name is configured
  docker_cmd="${docker_cmd} --build-arg AVALANCHEGO_NODE_IMAGE=${AVALANCHEGO_NODE_IMAGE}"

  # When building uninstrumented (arm64) with the unified Dockerfile, specify the target stage.
  local target_flag=""
  if [[ "$(go env GOARCH)" == "arm64" && -n "${uninstrumented_node_target}" ]]; then
    target_flag="--target=${uninstrumented_node_target}"
  fi

  # Build node image first to allow the workload image to use it.
  # shellcheck disable=SC2086 # target_flag intentionally unquoted to expand to nothing when empty
  ${docker_cmd} ${target_flag} -t "${node_image_name}" -f "${node_dockerfile}" "${target_path}"

  if [[ -n "${node_only}" ]]; then
    # Skip building the config and workload images. Supports building the avalanchego node image as the
    # base image for a VM node image.
    return
  fi

  # Build the config image
  ${docker_cmd} -t "${config_image_name}" -f "${base_dockerfile}.config" "${target_path}"

  # Build the workload image
  ${docker_cmd} -t "${workload_image_name}" -f "${base_dockerfile}.workload" "${target_path}"
}

# Generate the docker compose configuration for the antithesis config image.
function gen_antithesis_compose_config {
  local image_tag=$1
  local exe_path=$2
  local target_path=$3
  local extra_compose_args=${4:-}

  if [[ -d "${target_path}" ]]; then
    # Ensure the target path is empty before generating the compose config
    rm -r "${target_path:?}"
  fi
  mkdir -p "${target_path}"

  # Define the env vars for the compose config generation
  local compose_env="TARGET_PATH=${target_path} IMAGE_TAG=${image_tag} ${extra_compose_args}"

  # Generate compose config for copying into the config image
  # shellcheck disable=SC2086
  env ${compose_env} go run "${exe_path}"
}
