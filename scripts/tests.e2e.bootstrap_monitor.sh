#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.bootstrap_monitor.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

CMD=kind-with-registry.sh

if ! command -v "${CMD}" &> /dev/null; then
  echo "kind-with-registry.sh not found, have you run 'nix develop'?"
  echo "To install nix: https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix"
  exit 1
fi

"${CMD}"

KUBECONFIG="$HOME/.kube/config" ./bin/ginkgo -v ./tests/fixture/bootstrapmonitor/e2e
