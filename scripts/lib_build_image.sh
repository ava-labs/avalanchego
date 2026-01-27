#!/usr/bin/env bash

# Shared functions for Docker image build scripts.

get_go_version() {
  go list -m -f '{{.GoVersion}}'
}

docker_login_if_needed() {
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "Logging in to Docker registry..."
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
}

# Configures Docker buildx mode and platform flags.
# - Multi-arch builds (comma in platforms) require registry and use --push
# - Single-arch with registry (slash in image name) uses --push
# - Single-arch local builds use --load
#
# Sets: DOCKER_BUILD_MODE_FLAGS, DOCKER_PLATFORM_FLAGS
configure_docker_build_mode() {
  local image_name="${1:-}"
  local platforms="${2:-}"

  # Reset output variables
  DOCKER_BUILD_MODE_FLAGS=""
  DOCKER_PLATFORM_FLAGS=""

  local is_multi_arch=0
  [[ "$platforms" == *,* ]] && is_multi_arch=1

  local has_registry=0
  [[ "$image_name" == *"/"* ]] && has_registry=1

  # Multi-arch requires registry
  if [[ $is_multi_arch -eq 1 && $has_registry -eq 0 ]]; then
    echo "ERROR: Multi-arch images must be pushed to a registry."
    echo "ERROR: Image name must include repository (e.g., 'myregistry/image' or 'dockerhub/image')"
    exit 1
  fi

  if [[ $has_registry -eq 1 ]]; then
    DOCKER_BUILD_MODE_FLAGS="--push"
    docker_login_if_needed
  else
    DOCKER_BUILD_MODE_FLAGS="--load"
  fi

  if [[ -n "$platforms" ]]; then
    DOCKER_PLATFORM_FLAGS="--platform=$platforms"
  fi
}
