#!/usr/bin/env bash
# jq variables and GitHub expressions must remain literal in the queries below.
# shellcheck disable=SC2016
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# Inspect the actual workflow configuration. Both E2E jobs prepare modules
# before hashing; neither later composite may restore them a second time.
for workflow in .github/workflows/go.yml .github/workflows/go-scheduled.yml; do
  yq -e '
    .jobs.e2e.steps as $steps |
    ([range(0; $steps | length) | select($steps[.].uses == "./.github/actions/setup-go-for-project")] | first) as $setup |
    ([range(0; $steps | length) | select($steps[.].id == "cache-key")] | first) as $hash |
    ($setup != null and $hash != null and $setup < $hash) and
    ([ $steps[] | select(.uses == "./.github/actions/validate-go-e2e-cache" or .uses == "./.github/actions/run-monitored-tmpnet-cmd") ] as $consumers |
      ($consumers | length) == 2 and
      all($consumers[]; .with.setup_go_module_cache == "false"))
  ' "${workflow}" > /dev/null
  echo "Single module preparation configured: ${workflow}"
done

# Reuse Nix only after the exact-hit validator completes successfully.
for workflow in .github/workflows/go.yml .github/workflows/go-scheduled.yml; do
  yq -e '
    any(.jobs.e2e.steps[]; .uses == "./.github/actions/validate-go-e2e-cache" and .id == "validate-e2e-cache") and
    any(.jobs.e2e.steps[]; .uses == "./.github/actions/run-monitored-tmpnet-cmd" and
      .with.nix_setup_complete == "${{ steps.validate-e2e-cache.outcome == '\''success'\'' }}")
  ' "${workflow}" > /dev/null
done

yq -e '
  .inputs.nix_setup_complete.default == "false" and
  any(.runs.steps[]; .id == "install-nix" and
    .if == "${{ inputs.nix_setup_complete != '\''true'\'' }}") and
  ([.runs.steps[] | select(.name == "Upload tmpnet data" or .name == "Collect kind diagnostics")] |
    length == 2 and all(.[]; .if | contains("(inputs.nix_setup_complete == '\''true'\'' || steps.install-nix.outcome == '\''success'\'')")))
' .github/actions/run-monitored-tmpnet-cmd/action.yml > /dev/null

# Check the actual forwarding expressions, not a copy of their policy.
yq -e '.inputs.setup_go_module_cache.default == "true" and
  any(.runs.steps[]; .uses == "./.github/actions/install-nix" and
    .with.setup_go_module_cache == "${{ inputs.setup_go_module_cache }}")' \
  .github/actions/validate-go-e2e-cache/action.yml > /dev/null

yq -e '.inputs.setup_go_module_cache.default == "auto"' \
  .github/actions/run-monitored-tmpnet-cmd/action.yml > /dev/null
expression="$(yq -r '.runs.steps[] | select(.id == "install-nix") |
  .with.setup_go_module_cache | ltrimstr("${{") | rtrimstr("}}") |
  gsub("inputs\\.setup_go_module_cache"; "$modules") |
  gsub("inputs\\.setup_bazel"; "$bazel")' \
  .github/actions/run-monitored-tmpnet-cmd/action.yml)"

check_policy() {
  local mode="$1" bazel_mode="$2" expected="$3" actual=false
  # This expression uses only string equality, &&, ||, and parentheses, which
  # have the same truth values in GitHub expressions and Bash [[ ... ]].
  if modules="${mode}" bazel="${bazel_mode}" bash -c "[[ ${expression} ]]"; then
    actual=true
  fi
  if [[ "${actual}" != "${expected}" ]]; then
    echo "module preparation (${mode}, bazel=${bazel_mode}): expected ${expected}, got ${actual}" >&2
    exit 1
  fi
}
check_policy auto false true
check_policy auto true false
check_policy true false true
check_policy true true true
check_policy false false false
check_policy false true false

# The initial preparation must still disable downloads for later Nix commands.
yq -e '.inputs["disable-go-proxy"].default == "true"' \
  .github/actions/setup-go-module-cache/action.yml > /dev/null
yq -e 'any(.runs.steps[]; .uses == "./.github/actions/setup-go-module-cache" and
  (.with["disable-go-proxy"] // "true") == "true")' \
  .github/actions/setup-go-for-project/action.yml > /dev/null

echo 'Go E2E setup tests passed'
