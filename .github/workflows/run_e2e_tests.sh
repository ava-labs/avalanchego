#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Testing specific variables
camino_testing_repo="chain4travel/camino-testing"
caminogo_byzantine_repo="chain4travel/camino-byzantine"

# Define camino-testing and camino-byzantine versions to use
camino_testing_image="chain4travel/camino-testing:master"
camino_byzantine_image="chain4travel/camino-byzantine:update-caminogo-v1.7.0"

# Fetch the images
# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Camino root directory
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh

# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Receives params for debug execution
testBatch="${1:-}"
shift 1

echo "Running Test Batch: ${testBatch}"

# pulling the camino-testing image
docker pull $camino_testing_image
docker pull $camino_byzantine_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current caminogo
source "$CAMINO_PATH"/scripts/build_image.sh

# Target built version to use in camino-testing
camino_image="$caminogo_dockerhub_repo:$current_branch"

echo "Execution Summary:"
echo ""
echo "Running Camino Image: ${camino_image}"
echo "Running Camino Image Tag: $current_branch"
echo "Running Camino Testing Image: ${camino_testing_image}"
echo "Running Camino Byzantine Image: ${camino_byzantine_image}"
echo "Git Commit ID : ${git_commit_id}"
echo ""

# >>>>>>>> camino-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"caminogoImage\":\"${camino_image}\",
    \"caminogoByzantineImage\":\"${camino_byzantine_image}\",
    \"testBatch\":\"${testBatch}\"
}"
# >>>>>>>> camino-testing custom parameters <<<<<<<<<<<<<

bash "$CAMINO_PATH/.kurtosis/kurtosis.sh" \
    --custom-params "${custom_params_json}" \
    ${1+"${@}"} \
    "${camino_testing_image}" 
