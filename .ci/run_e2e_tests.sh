LATEST_KURTOSIS_TAG="kurtosistech/kurtosis:latest"
LATEST_CONTROLLER_TAG="kurtosistech/ava-test-controller:latest"

#docker pull ${LATEST_CONTROLLER_TAG}

SCRIPTS_PATH=$(cd $(dirname "${BASH_SOURCE[0]}"); pwd)
SRC_PATH=$(dirname "${SCRIPTS_PATH}")

# build docker image we need
bash "${SRC_PATH}"/scripts/build_image.sh
# get docker image label
GECKO_IMAGE=$(docker image ls --format="{{.Repository}}" | head -n 1)

go get -d -t -v github.com/kurtosis-tech/ava-e2e-tests/...

cd "${E2E_TEST_HOME}" || exit

./scripts/full_rebuild_and_run.sh

#kurtosis_pid=$!
#
#sleep 90
#kill ${kurtosis_pid}
#
#ACTUAL_EXIT_STATUS=$(docker ps -a --latest --filter ancestor=${LATEST_CONTROLLER_TAG} --format="{{.Status}}")
#EXPECTED_EXIT_STATUS="Exited \(0\).*"
#
## Clear containers.
#echo "Clearing kurtosis testnet containers."
#docker rm $(docker stop $(docker ps -a -q --filter ancestor="${GECKO_IMAGE}" --format="{{.ID}}")) >/dev/null
#
#if [[ ${ACTUAL_EXIT_STATUS} =~ ${EXPECTED_EXIT_STATUS} ]]
#then
#  echo "Kurtosis test succeeded."
#  exit 0
#else
#  echo "Kurtosis test failed."
#  exit 1
#fi
