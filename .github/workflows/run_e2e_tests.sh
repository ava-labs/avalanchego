#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Testing specific variables
avalanche_testing_repo="avaplatform/avalanche-testing"
avalanchego_byzantine_repo="avaplatform/avalanche-byzantine"

# Define avalanche-testing and avalanche-byzantine versions to use
avalanche_testing_image="avaplatform/avalanche-testing:master"
avalanchego_byzantine_image="avaplatform/avalanche-byzantine:update-avalanchego-v1.7.0"

# Fetch the images
# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Receives params for debug execution
testBatch="${1:-}"
shift 1

echo "Running Test Batch: ${testBatch}"

# pulling the avalanche-testing image
docker pull $avalanche_testing_image
docker pull $avalanchego_byzantine_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current avalanchego
source "$AVALANCHE_PATH"/scripts/build_image.sh

# Target built version to use in avalanche-testing
avalanche_image="$avalanchego_dockerhub_repo:$current_branch"

echo "Execution Summary:"
echo ""
echo "Running Avalanche Image: ${avalanche_image}"
echo "Running Avalanche Image Tag: $current_branch"
echo "Running Avalanche Testing Image: ${avalanche_testing_image}"
echo "Running Avalanche Byzantine Image: ${avalanchego_byzantine_image}"
echo "Git Commit ID : ${git_commit_id}"
echo ""

# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"avalanchegoImage\":\"${avalanche_image}\",
    \"avalanchegoByzantineImage\":\"${avalanchego_byzantine_image}\",
    \"testBatch\":\"${testBatch}\"
}"
# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<

bash "$AVALANCHE_PATH/.kurtosis/kurtosis.sh" \
    --custom-params "${custom_params_json}" \
    ${1+"${@}"} \
    "${avalanche_testing_image}" 
