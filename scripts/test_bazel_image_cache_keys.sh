#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
mkdir -p .cache/bazel-image
root="$(mktemp -d "$repo_root/.cache/bazel-image/test-keys.XXXXXX")"
trap 'rm -rf "$root"' EXIT
export RUNNER_OS=Linux RUNNER_ARCH=ARM64 GITHUB_OUTPUT="$root/initial"

# Source the real key generator to get its input list. Run every subsequent
# check in an isolated copy so mutations cannot affect repository files.
# shellcheck source=scripts/workflow-avalanchego-image-docker-cache-key.sh
source scripts/workflow-avalanchego-image-docker-cache-key.sh
for input in "${dependency_inputs[@]}"; do
  mkdir -p "$root/fixture/$(dirname "$input")"
  cp "$input" "$root/fixture/$input"
done
cd "$root/fixture"
script="$repo_root/scripts/workflow-avalanchego-image-docker-cache-key.sh"
GITHUB_OUTPUT="$root/same" bash "$script"
cmp "$root/initial" "$root/same"

value() {
  grep "^$2=" "$1" | cut -d= -f2-
}

# Different host architectures must never share archives or dependency entries.
RUNNER_ARCH=X64 GITHUB_OUTPUT="$root/x64" bash "$script"
for key in key dependency-key prefix dependency-prefix; do
  [[ "$(value "$root/initial" "$key")" != "$(value "$root/x64" "$key")" ]]
done

# Go graph changes invalidate dependencies but retain the Docker archive key.
printf '\n# test mutation\n' >> go.sum
GITHUB_OUTPUT="$root/go" bash "$script"
[[ "$(value "$root/initial" key)" == "$(value "$root/go" key)" ]]
[[ "$(value "$root/initial" dependency-key)" != "$(value "$root/go" dependency-key)" ]]

# Target graph changes can require more modules without changing go.mod.
printf '\n# test mutation\n' >> utils/formatting/BUILD.bazel
GITHUB_OUTPUT="$root/graph" bash "$script"
[[ "$(value "$root/go" key)" == "$(value "$root/graph" key)" ]]
[[ "$(value "$root/go" dependency-key)" != "$(value "$root/graph" dependency-key)" ]]

# Builder changes invalidate both exact keys, while preserving fallback prefixes.
printf '\n# test mutation\n' >> MODULE.bazel
GITHUB_OUTPUT="$root/builder" bash "$script"
for key in key dependency-key; do
  [[ "$(value "$root/graph" "$key")" != "$(value "$root/builder" "$key")" ]]
done
for key in prefix dependency-prefix; do
  [[ "$(value "$root/initial" "$key")" == "$(value "$root/builder" "$key")" ]]
done
[[ "$(value "$root/builder" key)" == "$(value "$root/builder" prefix)"* ]]
[[ "$(value "$root/builder" dependency-key)" == "$(value "$root/builder" dependency-prefix)"* ]]
echo 'Bazel image cache key tests passed'
