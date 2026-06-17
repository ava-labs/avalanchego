#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
impactedtests_bin="$("${script_dir}/bazel_built_file.sh" //tools/impactedtests:impactedtests_bin)"
targeted_bazel_bin="$("${script_dir}/bazel_built_file.sh" //tools/targeted-bazel:targeted_bazel_bin)"

IMPACTEDTESTS_BIN="$impactedtests_bin" exec "$targeted_bazel_bin" "$@"
