#!/usr/bin/env bash

set -euo pipefail

# Validates the compose configuration of the antithesis config image
# identified by IMAGE_NAME and IMAGE_TAG by:
#
#   1. Extracting the docker compose configuration from the image
#   2. Running the workload and its target network without error for a minute
#   3. Stopping the workload and its target network
#
# This script is intended to be sourced rather than executed directly.

if [[ -z "${IMAGE_NAME:-}" || -z "${IMAGE_TAG:-}" ]]; then
  echo "IMAGE_NAME and IMAGE_TAG must be set"
  exit 1
fi

# Create a container from the config image to extract compose configuration from
CONTAINER_NAME="tmp-${IMAGE_NAME}"
docker create --name "${CONTAINER_NAME}" "${IMAGE_NAME}:${IMAGE_TAG}" /bin/true

# Create a temporary directory to write the compose configuration to
TMPDIR="$(mktemp -d)"
echo "using temporary directory ${TMPDIR} as the docker compose path"

COMPOSE_FILE="${TMPDIR}/docker-compose.yml"
COMPOSE_CMD="docker compose -f ${COMPOSE_FILE}"

# Ensure cleanup
function cleanup {
  echo "removing temporary container"
  docker rm "${CONTAINER_NAME}"
  echo "stopping and removing the docker compose project"
  ${COMPOSE_CMD} down --volumes
  if [[ -z "${DEBUG:-}" ]]; then
    echo "removing temporary dir"
    rm -rf "${TMPDIR}"
  fi
}
trap cleanup EXIT

# Copy the docker-compose.yml file out of the container
docker cp "${CONTAINER_NAME}":/docker-compose.yml "${COMPOSE_FILE}"

# Copy the volume paths out of the container
docker cp "${CONTAINER_NAME}":/volumes "${TMPDIR}/"

# Run the docker compose project for 30 seconds without error. Local
# network bootstrap is ~6s, but github workers can be much slower.
${COMPOSE_CMD} up -d
sleep 30
if ${COMPOSE_CMD} ps -q | xargs docker inspect -f '{{ .State.Status }}' | grep -v 'running'; then
  echo "An error occurred."
  exit 1
fi

echo "Successfully invoked the antithesis test setup configured by ${IMAGE_NAME}:${IMAGE_TAG}"
