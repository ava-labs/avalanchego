LATEST_KURTOSIS_TAG="kurtosistech/kurtosis:latest"
LATEST_CONTROLLER_TAG="kurtosistech/ava-test-controller:latest"
GECKO_IMAGE="${DOCKERHUB_REPO}":"$COMMIT"

docker pull ${LATEST_CONTROLLER_TAG}
docker pull ${LATEST_KURTOSIS_TAG}

(docker run -v /var/run/docker.sock:/var/run/docker.sock \
--env DEFAULT_GECKO_IMAGE="${DEFAULT_GECKO_IMAGE}" \
--env TEST_CONTROLLER_IMAGE="${LATEST_CONTROLLER_TAG}" \
${LATEST_KURTOSIS_TAG}) &

kurtosis_pid=$!

sleep 90
kill ${kurtosis_pid}

ACTUAL_EXIT_STATUS=$(docker ps -a --latest --filter ancestor=${LATEST_CONTROLLER_TAG} --format="{{.Status}}")
EXPECTED_EXIT_STATUS="Exited \(0\).*"

echo "${ACTUAL_EXIT_STATUS}"

# Clear containers.
echo "Clearing kurtosis testnet containers."
docker rm $(docker stop $(docker ps -a -q --filter ancestor="${GECKO_IMAGE}" --format="{{.ID}}")) >/dev/null

if [[ ${ACTUAL_EXIT_STATUS} =~ ${EXPECTED_EXIT_STATUS} ]]
then
  echo "Kurtosis test succeeded."
  exit 0
else
  echo "Kurtosis test failed."
  exit 1
fi
