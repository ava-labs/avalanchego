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
# - Multi-arch builds (comma in platforms) require push to registry
# - Use PUSH=1 to push to registry, empty/unset to load locally
#
# Sets: DOCKER_BUILD_MODE_FLAGS, DOCKER_PLATFORM_FLAGS
configure_docker_build_mode() {
  local platforms="${1:-}"
  local push="${2:-}"

  # Reset output variables
  # shellcheck disable=SC2034  # Variables used by caller
  DOCKER_BUILD_MODE_FLAGS=""
  # shellcheck disable=SC2034  # Variables used by caller
  DOCKER_PLATFORM_FLAGS=""

  local is_multi_arch=0
  [[ "$platforms" == *,* ]] && is_multi_arch=1

  # Multi-arch requires push (can't load multi-arch images locally)
  if [[ $is_multi_arch -eq 1 ]]; then
    if [[ -z "$push" ]]; then
      echo "ERROR: Multi-arch images cannot be loaded locally."
      echo "ERROR: Set PUSH=1 to push to a registry."
      exit 1
    fi
  fi

  if [[ -n "$push" ]]; then
    # shellcheck disable=SC2034  # Variables used by caller
    DOCKER_BUILD_MODE_FLAGS="--push"
    docker_login_if_needed
  else
    # shellcheck disable=SC2034  # Variables used by caller
    DOCKER_BUILD_MODE_FLAGS="--load"
  fi

  if [[ -n "$platforms" ]]; then
    # shellcheck disable=SC2034  # Variables used by caller
    DOCKER_PLATFORM_FLAGS="--platform=$platforms"
  fi
}
