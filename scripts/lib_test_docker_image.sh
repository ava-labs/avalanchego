#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

# This script defines a helper function for Docker image test scripts. It is
# intended to be sourced rather than executed.

# Start a local Docker registry and multiplatform builder for testing multi-arch
# image builds. Sets REGISTRY_PORT as a side effect for use by the caller.
function start_test_registry {
  echo "starting local docker registry to allow verification of multi-arch image builds"
  REGISTRY_CONTAINER_ID="$(docker run --rm -d -P registry:2)"
  REGISTRY_PORT="$(docker port "$REGISTRY_CONTAINER_ID" 5000/tcp | grep -v "::" | awk -F: '{print $NF}')"

  echo "starting docker builder that supports multiplatform builds"
  # - creating a new builder enables multiplatform builds
  # - '--driver-opt network=host' enables the builder to use the local registry
  docker buildx create --use --name ci-builder --driver-opt network=host

  # Ensure registry and builder cleanup on teardown
  function cleanup {
    echo "stopping local docker registry"
    docker stop "${REGISTRY_CONTAINER_ID}"
    echo "removing multiplatform builder"
    docker buildx rm ci-builder
  }
  trap cleanup EXIT
}
