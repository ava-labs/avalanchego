SCRIPTS_PATH=$(cd $(dirname "${BASH_SOURCE[0]}"); pwd)
SRC_PATH=$(dirname "${SCRIPTS_PATH}")
# Build the runnable Gecko docker image
bash "${SRC_PATH}"/scripts/build_image.sh
GECKO_IMAGE=$(docker image ls --format="{{.Repository}}" | head -n 1)

# Turn off GO111MODULE to pull e2e test source code in order to get run script.
GO111MODULE=off go get -t -v github.com/kurtosis-tech/ava-e2e-tests/...
cd "${GOPATH}"/src/github.com/kurtosis-tech/ava-e2e-tests/ || exit
./scripts/full_rebuild_and_run.sh
