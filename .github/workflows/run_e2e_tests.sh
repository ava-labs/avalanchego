set -o errexit
set -o nounset
set -o pipefail

root_dirpath="$(cd "$(dirname "${0}")" && cd ../../ && pwd)"
kurtosis_core_dirpath="${root_dirpath}/.kurtosis"

echo "root_dirpath: ${root_dirpath}"
echo "kurtosis_core_dirpath: ${kurtosis_core_dirpath}"

# Define avalanche byzantine version to use
avalanche_byzantine_image="avaplatform/avalanche-byzantine:v0.2.0-rc.1"
# Define avalanche testing version to use
avalanche_testing_image="avaplatform/avalanche-testing:kurtosis_upgrade"

# Fetch the images
# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Byzantine Tests because Docker Credentials were not present."
    exit 1
else
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
    docker pull ${avalanche_testing_image}
    docker pull ${avalanche_byzantine_image}
fi

# Setting the build ID
GIT_COMMIT_ID=$( git rev-list -1 HEAD )

# Build current avalanchego
"${root_dirpath}"/scripts/build_image.sh

# Target built version to use in avalanche-testing
# Todo hook this with the e2e matchup
#avalanche_image_tag=$(docker image ls --format="{{.Tag}}" | head -n 1)
avalanche_image_tag="dev"
avalanche_image="avaplatform/avalanchego:${avalanche_image_tag}"

echo "Running Avalanche Image: ${avalanche_image}"
echo "Running Avalanche Image Tag: ${avalanche_image_tag}"
echo "Running Avalanche Testing Image: ${avalanche_testing_image}"
echo "Running Avalanche Byzantine Image: ${avalanche_byzantine_image}"
echo "Git Commit ID : ${GIT_COMMIT_ID}"


# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"avalanchegoImage\":\"${avalanche_image}\",
    \"avalanchegoByzantineImage\":\"${avalanche_byzantine_image}\"
}"
# >>>>>>>> avalanche-testing custom parameters <<<<<<<<<<<<<

bash "${kurtosis_core_dirpath}/kurtosis.sh" \
    --custom-params "${custom_params_json}" \
    "${avalanche_testing_image}"
