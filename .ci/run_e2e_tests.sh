SCRIPTS_PATH=$(cd $(dirname "${BASH_SOURCE[0]}"); pwd)
SRC_PATH=$(dirname "${SCRIPTS_PATH}")
# Build the runnable Gecko docker image
bash "${SRC_PATH}"/scripts/build_image.sh
GECKO_IMAGE=$(docker image ls --format="{{.Repository}}" | head -n 1)

# login to AWS for byzantine images
export AWS_ACCESS_KEY_ID="${BYZ_REG_AWS_ID}"
export AWS_SECRET_ACCESS_KEY="${BYZ_REG_AWS_KEY}"
export AWS_DEFAULT_REGION="${BYZ_REG_AWS_REGION}"
aws ecr get-login-password --region "${AWS_DEFAULT_REGION}" | docker login --username AWS --password-stdin 964377072876.dkr.ecr.us-east-1.amazonaws.com
CHIT_SPAMMER_IMAGE="964377072876.dkr.ecr.us-east-1.amazonaws.com/gecko-byzantine:latest"

docker pull "${CHIT_SPAMMER_IMAGE}"

# Turn off GO111MODULE to pull e2e test source code in order to get run script.
git clone https://github.com/kurtosis-tech/ava-e2e-tests.git
cd ava-e2e-tests/ || exit
git checkout 43285f873e0f19029f0e9ed7fbd845a411d6de2b

bash "./scripts/rebuild_initializer_binary.sh"
bash "./scripts/rebuild_controller_image.sh"
# TODO: Make the controller image label a parameter to rebuild_controller_image script
# Standard controller image label used by above scripts.
CONTROLLER_IMAGE="kurtosistech/ava-e2e-tests_controller:latest"
./build/ava-e2e-tests --gecko-image-name="${GECKO_IMAGE}" --test-controller-image-name="${CONTROLLER_IMAGE}" --chit-spammer-image-name="${CHIT_SPAMMER_IMAGE}"
