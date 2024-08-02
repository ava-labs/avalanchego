#!/usr/bin/env bash

set -euo pipefail

# Validates the construction of the antithesis images for a test setup specified by TEST_SETUP.
#
#   1. Building the antithesis test image
#   2. Extracting the docker compose configuration from the image
#   3. Running the workload and its target network without error for a minute
#   4. Stopping the workload and its target network
#
# `docker compose` is used (docker compose v2 plugin) due to it being installed by default on
# public github runners. `docker-compose` (the v1 plugin) is not installed by default.

# e.g.,
# TEST_SETUP=avalanchego ./scripts/tests.build_antithesis_images.sh                 # Test build of images for avalanchego test setup
# DEBUG=1 TEST_SETUP=avalanchego ./scripts/tests.build_antithesis_images.sh         # Retain the temporary compose path for troubleshooting

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Discover the default tag that will be used for the image
source "${AVALANCHE_PATH}"/scripts/constants.sh
export TAG="${commit_hash}"

# Build the images for the specified test setup
export TEST_SETUP="${TEST_SETUP:-}"
bash -x "${AVALANCHE_PATH}"/scripts/build_antithesis_images.sh

# Create a container from the config image to extract compose configuration from
IMAGE_NAME="antithesis-${TEST_SETUP}-config"
CONTAINER_NAME="tmp-${IMAGE_NAME}"
docker create --name "${CONTAINER_NAME}" "${IMAGE_NAME}:${TAG}" /bin/true

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
  exit 255
fi

# Success!
