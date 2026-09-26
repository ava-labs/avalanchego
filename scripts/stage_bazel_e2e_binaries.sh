#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]] || [[ "$1" != "avalanchego" && "$1" != "runtime" ]]; then
  echo "usage: $0 <avalanchego|runtime>" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
staging_dir="${repo_root}/build/bazel-e2e"

stage_binary() {
  local target="$1"
  local destination="$2"
  local source

  source="$(bazelisk cquery --output=files "${target}")"
  [[ -n "${source}" ]] || {
    echo "error: Bazel did not produce a file for ${target}" >&2
    exit 1
  }
  cp "${source}" "${destination}"
}

case "$1" in
  avalanchego)
    rm -rf "${staging_dir}/avalanchego"
    mkdir -p "${staging_dir}/avalanchego"
    stage_binary //main:avalanchego "${staging_dir}/avalanchego/avalanchego"
    ;;
  runtime)
    rm -rf "${staging_dir}/runtime"
    mkdir -p "${staging_dir}/runtime"
    stage_binary //tests/e2e:e2e_test_binary "${staging_dir}/runtime/e2e.test"
    stage_binary @com_github_onsi_ginkgo_v2//ginkgo:ginkgo "${staging_dir}/runtime/ginkgo"
    stage_binary //vms/example/xsvm/cmd/xsvm:xsvm "${staging_dir}/runtime/xsvm"
    ;;
esac
