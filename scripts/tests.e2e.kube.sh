#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh

# TODO(marun) Factor out ginkgo installation to avoid duplicating it across test scripts
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.13.1

# TODO(marun) Is the path still necessary?
KUBECONFIG="$HOME/.kube/config" PATH="${PWD}/bin:$PATH" ginkgo -v ./tests/e2e -- --runtime=kube
