#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.bootstrap_monitor.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh "$@"
./bin/ginkgo -v ./tests/fixture/bootstrapmonitor/e2e "$@"
