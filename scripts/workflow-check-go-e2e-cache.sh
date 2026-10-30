#!/usr/bin/env bash
set -euo pipefail

# Run inside the same Nix environment as the E2E task, before it warms GOCACHE.
case "${1:-}" in
  race) race=(-race) ;;
  no-race) race=() ;;
  *) echo "usage: $0 <race|no-race>" >&2; exit 2 ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
source ./scripts/constants.sh
source ./scripts/git_commit.sh
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

check() {
  echo "Checking cached compilation: $*"
  if ! "$@" > "${workdir}/dry-run.log" 2>&1; then
    cat "${workdir}/dry-run.log" >&2
    return 1
  fi
  ./scripts/workflow-validate-go-build-cache.sh "${workdir}/dry-run.log"
}

check go build -n "${race[@]}" -o "${workdir}/avalanchego" \
  -ldflags "-X github.com/ava-labs/avalanchego/version.GitCommit=$git_commit $static_ld_flags" ./main
# The existing E2E tasks build xsvm and the E2E test binary without -race.
check go build -n -o "${workdir}/xsvm" ./vms/example/xsvm/cmd/xsvm/
# go tool uses the same compilation mode as go run, not go build.
check go run -n github.com/onsi/ginkgo/v2/ginkgo
check go test -n -c -o "${workdir}/e2e.test" ./tests/e2e
