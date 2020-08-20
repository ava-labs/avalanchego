SCRIPTS_PATH=$(cd $(dirname "${BASH_SOURCE[0]}"); pwd)
SRC_PATH=$(dirname "${SCRIPTS_PATH}")
# Build the runnable Gecko docker image
bash "${SRC_PATH}"/scripts/build_image.sh
GECKO_IMAGE=$(docker image ls --format="{{.Repository}}" | head -n 1)

DOCKER_REPO="avaplatform"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

TESTING_CONTROLLER_IMAGE="$DOCKER_REPO/avalanche-e2e-tests_controller:everest-latest"
BYZANTINE_IMAGE="$DOCKER_REPO/gecko-byzantine:everest-latest"

docker pull "$TESTING_CONTROLLER_IMAGE"
docker pull "${BYZANTINE_IMAGE}"


E2E_TESTING_REMOTE="https://github.com/ava-labs/avalanche-testing.git"
E2E_TAG="v0.8.2-dev"

mkdir -p "$E2E_TEST_HOME"
git clone "$E2E_TESTING_REMOTE" "$E2E_TEST_HOME"
cd "$E2E_TEST_HOME" || exit
git fetch origin --tags
git checkout "tags/$E2E_TAG" -b "$E2E_TAG"

go mod edit -replace github.com/ava-labs/gecko="$GECKO_HOME"
bash "./scripts/rebuild_initializer_binary.sh"
./build/avalanche-e2e-tests --gecko-image-name="${GECKO_IMAGE}" --test-controller-image-name="${TESTING_CONTROLLER_IMAGE}" --byzantine-image-name="${BYZANTINE_IMAGE}"
