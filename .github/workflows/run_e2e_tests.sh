set -o errexit
set -o nounset
set -o pipefail

# Testing specific variables
avalanche_testing_repo="avaplatform/avalanche-testing"

# Define avalanche byzantine version to use
avalanchego_byzantine_version=${AVALANCHEGO_BYZANTINE_VERSION:-'v0.2.1'}

# Define avalanche testing version to use
avalanche_testing_image="avaplatform/avalanche-testing:master"

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

# pulling the avalanche-testing image
docker pull $avalanche_testing_image
docker pull $avalanchego_byzantine_version

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current avalanchego
"$AVALANCHE_PATH"/scripts/build_image.sh

# Target built version to use in avalanche-testing
avalanche_image="avaplatform/avalanchego:$current_branch"

echo "Running Avalanche Image: ${avalanche_image}"
echo "Running Avalanche Image Tag: $current_branch"
echo "Running Avalanche Testing Image: ${avalanche_testing_image}"
echo "Running Avalanche Byzantine Image: ${avalanchego_byzantine_version}"
echo "Git Commit ID : ${git_commit_id}"


# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"avalanchegoImage\":\"${avalanche_image}\",
    \"avalanchegoByzantineImage\":\"${avalanchego_byzantine_version}\"
}"
# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<

bash "$AVALANCHE_PATH/.kurtosis/kurtosis.sh" \
    --custom-params "${custom_params_json}" \
    "${avalanche_testing_image}" \
    $@
