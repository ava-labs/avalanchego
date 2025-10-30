#!/usr/bin/env bash

set -euo pipefail

# Define Matrix - Generates job matrix based on event type and inputs
#
# Usage:
#   ./c-chain-reexecution-matrix.sh
#
# Environment Variables:
#   GITHUB_EVENT_NAME - The event that triggered the workflow (workflow_dispatch, pull_request, schedule)
#   WORKFLOW_DISPATCH_TASK - Task name from workflow_dispatch input
#   WORKFLOW_DISPATCH_RUNNER - Runner name from workflow_dispatch input
#   WORKFLOW_DISPATCH_TIMEOUT - Timeout in minutes from workflow_dispatch input
#   CONFIG_FILE - Path to the configuration JSON file
#
# Outputs:
#   Sets GITHUB_OUTPUT with matrix-native and matrix-self-hosted

# Runners considered "native" (not self-hosted)
NATIVE_RUNNERS=("ubuntu-latest" "blacksmith-4vcpu-ubuntu-2404")

is_native_runner() {
  local runner="$1"
  for native in "${NATIVE_RUNNERS[@]}"; do
    [[ "$runner" == "$native" ]] && return 0
  done
  return 1
}

write_output() {
  local native_matrix="$1"
  local self_hosted_matrix="$2"

  {
    echo "matrix-native<<EOF"
    echo "$native_matrix"
    echo "EOF"
    echo "matrix-self-hosted<<EOF"
    echo "$self_hosted_matrix"
    echo "EOF"
  } >> "$GITHUB_OUTPUT"
}

# Handle workflow_dispatch event
if [[ "$GITHUB_EVENT_NAME" == "workflow_dispatch" ]]; then
  task="$WORKFLOW_DISPATCH_TASK"
  runner="$WORKFLOW_DISPATCH_RUNNER"
  timeout="$WORKFLOW_DISPATCH_TIMEOUT"

  if is_native_runner "$runner"; then
    native_matrix=$(jq -c \
      --arg t "$task" \
      --arg r "$runner" \
      --argjson tm "$timeout" \
      '{include:[{task:$t,runner:$r,"timeout-minutes":$tm}]}')
    self_hosted_matrix='{"include":[]}'
  else
    native_matrix='{"include":[]}'
    self_hosted_matrix=$(jq -c \
      --arg t "$task" \
      --arg r "$runner" \
      --argjson tm "$timeout" \
      '{include:[{task:$t,runner:$r,"timeout-minutes":$tm}]}')
  fi

  write_output "$native_matrix" "$self_hosted_matrix"
  exit 0
fi

# Handle pull_request or schedule events
# Read from config and split by the 'self_hosted' flag
full_matrix=$(jq -r ".\"$GITHUB_EVENT_NAME\"" "$CONFIG_FILE")

native_matrix=$(echo "$full_matrix" | jq -c '{include: [.include[] | select(.self_hosted == false)]}')
self_hosted_matrix=$(echo "$full_matrix" | jq -c '{include: [.include[] | select(.self_hosted == true)]}')

write_output "$native_matrix" "$self_hosted_matrix"
