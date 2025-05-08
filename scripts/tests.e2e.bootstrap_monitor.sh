#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.
#
# --kubeconfig and --kubeconfig-context should be provided in the form --arg=value
# to work with the simplistic mechanism enabling flag reuse.

if ! [[ "$0" =~ scripts/tests.e2e.bootstrap_monitor.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh "$@"
./bin/ginkgo -v ./tests/fixture/bootstrapmonitor/e2e "$@"
