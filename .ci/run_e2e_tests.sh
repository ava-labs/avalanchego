SCRIPTS_PATH=$(cd $(dirname "${BASH_SOURCE[0]}"); pwd)
SRC_PATH=$(dirname "${SCRIPTS_PATH}")
# Build the runnable Avalanche docker image
bash "${SRC_PATH}"/scripts/build_image.sh
AVALANCHE_IMAGE=$(docker image ls --format="{{.Repository}}" | head -n 1)

DOCKER_REPO="avaplatform"

E2E_TESTING_REMOTE="https://github.com/ava-labs/avalanche-testing.git"
E2E_TAG="v0.9.2-dev"

mkdir -p "$E2E_TEST_HOME"
git clone "$E2E_TESTING_REMOTE" "$E2E_TEST_HOME"
cd "$E2E_TEST_HOME" || exit
git fetch origin --tags
git checkout "tags/$E2E_TAG" -b "$E2E_TAG"

go mod edit -replace github.com/ava-labs/avalanche-go="$AVALANCHE_HOME"
bash "./scripts/rebuild_initializer_binary.sh"


TESTING_CONTROLLER_IMAGE="$DOCKER_REPO/avalanche-testing_controller:everest-v0.9.2-dev"
BYZANTINE_IMAGE="$DOCKER_REPO/gecko-byzantine:everest-name-change"

docker pull "$TESTING_CONTROLLER_IMAGE"

# If Docker Credentials are not available skip the Byzantine Tests
if [[ ${#DOCKER_USERNAME} == 0 ]]; then
    echo "Skipping Byzantine Tests because Docker Credentials were not present."
    ./build/avalanche-testing --avalanche-image-name="${AVALANCHE_IMAGE}" --test-controller-image-name="${TESTING_CONTROLLER_IMAGE}"
else
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
    docker pull "${BYZANTINE_IMAGE}"
    ./build/avalanche-testing --avalanche-image-name="${AVALANCHE_IMAGE}" --test-controller-image-name="${TESTING_CONTROLLER_IMAGE}" --byzantine-image-name="${BYZANTINE_IMAGE}"
fi
