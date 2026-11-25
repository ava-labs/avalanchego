#!/usr/bin/env bash

set -euo pipefail

# Bazel supplies the test binary, Ginkgo, and tmpnetctl as runfiles.
if (( $# < 3 )); then
  echo "usage: $0 <e2e.test> <ginkgo> <tmpnetctl> [e2e-test-args...]" >&2
  exit 1
fi
E2E_TARGET="$(realpath "$1")"
GINKGO_PATH="$(realpath "$2")"
TMPNETCTL_PATH="$(realpath "$3")"
export E2E_TARGET GINKGO_PATH TMPNETCTL_PATH
shift 3
cd "${BUILD_WORKSPACE_DIRECTORY:-${PWD}}"

# The registry must exist before the image is pushed. The kube script reuses
# the cluster and passes the selected image to tmpnet.
./scripts/start_kind_cluster.sh "$@"
./scripts/build_bazel_image.sh
if [[ "${BAZEL_IMAGE##*/}" != *:* ]]; then
  echo 'BAZEL_IMAGE must include an explicit tag' >&2
  exit 1
fi
push_output="$(docker push "${BAZEL_IMAGE}")"
printf '%s\n' "${push_output}"
digest="$(awk '$2 == "digest:" { print $3 }' <<<"${push_output}")"
if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo 'Docker push did not report a registry manifest digest' >&2
  exit 1
fi
export KUBE_AVALANCHEGO_IMAGE="${BAZEL_IMAGE%:*}@${digest}"
echo "Running kube e2e with pushed image ${KUBE_AVALANCHEGO_IMAGE}"
exec ./scripts/tests.e2e.kube.sh "$@"
