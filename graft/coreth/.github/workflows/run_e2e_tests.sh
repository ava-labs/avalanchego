set -o errexit
set -o nounset
set -o pipefail

# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Testing specific variables
avalanche_testing_repo="avaplatform/avalanche-testing"
avalanchego_repo="avaplatform/avalanchego"
# Define default avalanche testing version to use
avalanche_testing_image="${avalanche_testing_repo}:master"

# Avalanche root directory
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$CORETH_PATH"/scripts/versions.sh

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Checks available docker tags exist
function docker_tag_exists() {
    TOKEN=$(curl -s -H "Content-Type: application/json" -X POST -d '{"username": "'${DOCKER_USERNAME}'", "password": "'${DOCKER_PASS}'"}' https://hub.docker.com/v2/users/login/ | jq -r .token)
    curl --silent -H "Authorization: JWT ${TOKEN}" -f --head -lL https://hub.docker.com/v2/repositories/$1/tags/$2/ > /dev/null
}

# Defines the avalanche-testing tag to use
# Either uses the same tag as the current branch or uses the default
if docker_tag_exists $avalanche_testing_repo $current_branch; then
    echo "$avalanche_testing_repo:$current_branch exists; using this image to run e2e tests"
    avalanche_testing_image="$avalanche_testing_repo:$current_branch"
else
    echo "$avalanche_testing_repo $current_branch does NOT exist; using the default image to run e2e tests"
fi

echo "Using $avalanche_testing_image for e2e tests"

# Defines the avalanchego tag to use
# Either uses the same tag as the current branch or uses the default
# Disable matchup in favor of explicit tag
# TODO re-enable matchup when our workflow better supports it.
# if docker_tag_exists $avalanchego_repo $current_branch; then
#     echo "$avalanchego_repo:$current_branch exists; using this avalanchego image to run e2e tests"
#     AVALANCHE_VERSION=$current_branch
# else
#     echo "$avalanchego_repo $current_branch does NOT exist; using the default image to run e2e tests"
# fi

# pulling the avalanche-testing image
docker pull $avalanche_testing_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current avalanchego
source "$CORETH_PATH"/scripts/build_image.sh

# Target built version to use in avalanche-testing
avalanche_image="avaplatform/avalanchego:$build_image_id"

echo "Running Avalanche Image: ${avalanche_image}"
echo "Running Avalanche Testing Image: ${avalanche_testing_image}"
echo "Git Commit ID : ${git_commit_id}"


# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"avalanchegoImage\":\"${avalanche_image}\",
    \"testBatch\":\"avalanchego\"
}"
# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<

bash "$CORETH_PATH/.kurtosis/kurtosis.sh" \
    --tests "C-Chain Bombard WorkFlow,Dynamic Fees,Snowman++ Correct Proposers and Timestamps" \
    --custom-params "${custom_params_json}" \
    "${avalanche_testing_image}" \
    $@
