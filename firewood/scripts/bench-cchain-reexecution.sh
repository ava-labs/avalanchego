#!/usr/bin/env bash
set -euo pipefail

# Triggers and monitors C-Chain re-execution benchmarks in AvalancheGo.

cmd_help() {
    cat <<'EOF'
Triggers and monitors C-Chain re-execution benchmarks in AvalancheGo.

USAGE
  ./bench-cchain-reexecution.sh <command> [args]

COMMANDS
  trigger [test]   Trigger benchmark, wait, download results
  status <run_id>  Check run status
  list             List recent runs
  tests            Show available tests
  help             Show this help message

ENVIRONMENT
  GH_TOKEN              GitHub token for API access (required)
  TEST                  Predefined test name, alternative to arg (optional)
  FIREWOOD_REF          Firewood commit/tag/branch, empty = AvalancheGo's go.mod default (optional)
  AVALANCHEGO_REF       AvalancheGo branch/tag to test against (default: master)
                        NOTE: Must be a branch or tag name, not a commit SHA (GitHub API limitation)
  RUNNER                GitHub Actions runner label (default: avalanche-avalanchego-runner-2ti)
  LIBEVM_REF            libevm ref (optional)
  TIMEOUT_MINUTES       Workflow timeout in minutes (optional)
  DOWNLOAD_DIR          Directory for downloaded artifacts (default: ./results)

  Custom mode (when no TEST/test arg specified):
  CONFIG                VM config (default: firewood)
  START_BLOCK           First block number (required)
  END_BLOCK             Last block number (required)
  BLOCK_DIR_SRC         S3 block directory, e.g., cchain-mainnet-blocks-200-ldb (required)
  CURRENT_STATE_DIR_SRC S3 state directory, empty = genesis run (optional)

EXAMPLES
  ./bench-cchain-reexecution.sh trigger firewood-101-250k

  TEST=firewood-33m-40m FIREWOOD_REF=v0.1.0 ./bench-cchain-reexecution.sh trigger

  START_BLOCK=101 END_BLOCK=250000 \
    BLOCK_DIR_SRC=cchain-mainnet-blocks-1m-ldb \
    CURRENT_STATE_DIR_SRC=cchain-current-state-mainnet-ldb \
    ./bench-cchain-reexecution.sh trigger

  ./bench-cchain-reexecution.sh status 12345678

TESTS
EOF
    list_tests
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

AVALANCHEGO_REPO="ava-labs/avalanchego"
WORKFLOW_NAME="C-Chain Re-Execution Benchmark w/ Container"

: "${FIREWOOD_REF:=}"
: "${AVALANCHEGO_REF:=master}"
: "${RUNNER:=avalanche-avalanchego-runner-2ti}"
: "${LIBEVM_REF:=}"
: "${TIMEOUT_MINUTES:=}"
: "${CONFIG:=firewood}"
: "${DOWNLOAD_DIR:=${REPO_ROOT}/results}"

# Polling config for workflow registration. gh workflow run doesn't return
# the run ID, so we poll until the run appears. 180s accommodates busy runners.
#
# TRIGGER_RUN_ID is global and ensuring gh errors propagate and fail the script.
TRIGGER_RUN_ID=""
POLL_INTERVAL=1
POLL_TIMEOUT=180

# Subset of tests for discoverability. AvalancheGo is the source of truth:
# https://github.com/ava-labs/avalanchego/blob/master/scripts/benchmark_cchain_range.sh
# Format: "description|poll_interval_seconds"
declare -A TESTS=(
    ["firewood-101-250k"]="Blocks 101-250k|10"
    ["firewood-archive-101-250k"]="Blocks 101-250k (archive)|10"
    ["firewood-33m-33m500k"]="Blocks 33m-33.5m|30"
    ["firewood-archive-33m-33m500k"]="Blocks 33m-33.5m (archive)|30"
    ["firewood-33m-40m"]="Blocks 33m-40m|300"
    ["firewood-archive-33m-40m"]="Blocks 33m-40m (archive)|300"
)

# Parse test metadata
get_test_description() { echo "${TESTS[$1]%%|*}"; }
get_test_poll_interval() { echo "${TESTS[$1]##*|}"; }

log() { echo "==> $1" >&2; }

err() {
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
        echo "::error::$1"
    else
        echo "error: $1" >&2
    fi
}

require_gh() {
    if ! command -v gh &>/dev/null; then
        err "gh CLI not found. Run from nix shell: nix develop ./ffi"; exit 1
    fi
    if [[ -z "${GH_TOKEN:-}" ]]; then
        err "GH_TOKEN is required"; exit 1
    fi
}

# Prevent command injection via malicious ref names passed to gh CLI
validate_ref() {
    local ref="$1" name="$2"
    [[ "$ref" =~ ^[a-zA-Z0-9/_.-]+$ ]] || { err "$name contains invalid characters: $ref"; exit 1; }
}

# Verify a run's inputs match what we triggered with
# Check if a workflow run's inputs match expected values.
# Returns: 0 if inputs match, 1 if mismatch
#
# NOTE: We rely on AvalancheGo's workflow job naming convention:
#   "c-chain-reexecution (start, end, block-dir, config, runner...)"
# If AvalancheGo changes this format, this matching logic will need updating.
run_inputs_match() {
    local run_id="$1" expected_test="$2" expected_config="$3" expected_start="$4" expected_end="$5" expected_runner="$6"
    
    # Get the c-chain-reexecution job name (second job, after define-matrix).
    # If gh fails (e.g., run doesn't exist yet), return 1 to try the next candidate.
    local job_name
    job_name=$(gh run view "$run_id" --repo "$AVALANCHEGO_REPO" --json jobs \
        --jq '.jobs[] | select(.name | startswith("c-chain-reexecution")) | .name // ""' 2>/dev/null) || return 1
    
    # If job hasn't started yet (only define-matrix exists), accept for now
    [[ -z "$job_name" ]] && return 0
    
    # If we have a test name, check if it appears in job name
    if [[ -n "$expected_test" ]]; then
        [[ "$job_name" == *"$expected_test"* ]] && return 0
        return 1
    fi
    
    # For custom mode, check that our params appear in job name.
    # Job name format from matrix: "c-chain-reexecution (start, end, block-dir, current-state-dir, config, runner)"
    # Example: "c-chain-reexecution (10000001, 20000000, cchain-mainnet-blocks-50m-ldb, cchain-current-state-firewood-10m, firewood, avago-runner-...)"
    local match=true
    [[ -n "$expected_start" && "$job_name" != *"$expected_start"* ]] && match=false
    [[ -n "$expected_end" && "$job_name" != *"$expected_end"* ]] && match=false
    [[ -n "$expected_config" && "$job_name" != *"$expected_config"* ]] && match=false
    [[ -n "$expected_runner" && "$job_name" != *"$expected_runner"* ]] && match=false
    
    $match && return 0
    
    # Fallback: if no identifiers to match, accept the run
    [[ -z "$expected_test" && -z "$expected_start" && -z "$expected_runner" ]] && return 0
    
    return 1
}

poll_workflow_registration() {
    local trigger_time="$1"
    local expected_test="${2:-}"
    local expected_config="${3:-}"
    local expected_start="${4:-}"
    local expected_end="${5:-}"
    local expected_runner="${6:-}"
    
    log "Waiting for workflow to register (looking for runs after $trigger_time)..."
    
    # Verify we can list runs (fail fast on permission issues)
    if ! gh run list --repo "$AVALANCHEGO_REPO" --workflow "$WORKFLOW_NAME" --limit 1 &>/dev/null; then
        err "Cannot list runs in $AVALANCHEGO_REPO. Check token permissions."; exit 1
    fi
    
    local last_error=""
    local consecutive_failures=0
    for ((i=0; i<POLL_TIMEOUT; i++)); do
        sleep "$POLL_INTERVAL"
        local raw_output
        raw_output=$(gh run list \
            --repo "$AVALANCHEGO_REPO" \
            --workflow "$WORKFLOW_NAME" \
            --limit 10 \
            --json databaseId,createdAt 2>&1) || {
            last_error="$raw_output"
            ((consecutive_failures++))
            log "gh run list failed ($consecutive_failures/5): $raw_output"
            if ((consecutive_failures >= 5)); then
                err "gh run list failed 5 times consecutively: $last_error"
                exit 1
            fi
            continue
        }
        consecutive_failures=0  # Reset on success
        
        # Debug: show first result on first iteration
        ((i == 0)) && log "Latest run: $(echo "$raw_output" | jq -c '.[0] // "none"')"
        
        # Get candidate runs (created after trigger_time), oldest first
        local candidates
        candidates=$(echo "$raw_output" | jq -r --arg t "$trigger_time" \
            '[.[] | select(.createdAt > $t)] | reverse | .[].databaseId') || continue
        
        # Check each candidate's inputs to find our run
        for run_id in $candidates; do
            if run_inputs_match "$run_id" "$expected_test" "$expected_config" "$expected_start" "$expected_end" "$expected_runner"; then
                TRIGGER_RUN_ID="$run_id"
                return 0
            fi
        done
        
        # Progress indicator every 10 seconds
        ((i % 10 == 0)) && ((i > 0)) && log "Polling (${i}s)"
    done
    
    local msg="Workflow not found after ${POLL_TIMEOUT}s."
    [[ -n "$last_error" ]] && msg+=" Last error: $last_error"
    msg+=" Check: https://github.com/$AVALANCHEGO_REPO/actions/workflows"
    err "$msg"; exit 1
}

trigger_workflow() {
    local test="${1:-}"
    local firewood="$FIREWOOD_REF"
    
    # Validate refs if provided
    if [[ -n "$firewood" ]]; then
        # Resolve HEAD to commit SHA for reproducibility
        [[ "$firewood" == "HEAD" ]] && firewood=$(git -C "$REPO_ROOT" rev-parse HEAD)
        validate_ref "$firewood" "FIREWOOD_REF"
    fi
    [[ -n "$LIBEVM_REF" ]] && validate_ref "$LIBEVM_REF" "LIBEVM_REF"
    
    local args=(-f runner="$RUNNER")
    [[ -n "$TIMEOUT_MINUTES" ]] && args+=(-f timeout-minutes="$TIMEOUT_MINUTES")
    
    # Build with-dependencies string (format: "firewood=abc,libevm=xyz")
    local deps=""
    [[ -n "$firewood" ]] && deps="firewood=$firewood"
    [[ -n "$LIBEVM_REF" ]] && deps="${deps:+$deps,}libevm=$LIBEVM_REF"
    [[ -n "$deps" ]] && args+=(-f with-dependencies="$deps")
    
    if [[ -n "$test" ]]; then
        args+=(-f test="$test")
        log "Triggering: $test"
    else
        # Custom mode: block params required, CURRENT_STATE_DIR_SRC optional (empty = genesis)
        if [[ -z "${START_BLOCK:-}${END_BLOCK:-}${BLOCK_DIR_SRC:-}" ]]; then
            err "Provide a test name or set START_BLOCK, END_BLOCK, BLOCK_DIR_SRC"
            exit 1
        fi
        : "${START_BLOCK:?START_BLOCK required}"
        : "${END_BLOCK:?END_BLOCK required}"
        : "${BLOCK_DIR_SRC:?BLOCK_DIR_SRC required}"
        args+=(
            -f config="$CONFIG"
            -f start-block="$START_BLOCK"
            -f end-block="$END_BLOCK"
            -f block-dir-src="$BLOCK_DIR_SRC"
        )
        [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]] && args+=(-f current-state-dir-src="$CURRENT_STATE_DIR_SRC")
        log "Triggering: $CONFIG $START_BLOCK-$END_BLOCK${CURRENT_STATE_DIR_SRC:+ (with state)}"
    fi
    
    log "avalanchego: $AVALANCHEGO_REF"
    log "firewood:    ${firewood:-<AvalancheGo go.mod default>}"
    log "runner:      $RUNNER"
    
    # Record time BEFORE triggering to avoid race condition: we only look for
    # runs created after this timestamp, so concurrent triggers don't collide
    local trigger_time
    trigger_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    gh workflow run "$WORKFLOW_NAME" \
        --repo "$AVALANCHEGO_REPO" \
        --ref "$AVALANCHEGO_REF" \
        "${args[@]}"
    
    # Pass test/custom params to help identify our specific run among concurrent triggers
    poll_workflow_registration "$trigger_time" "$test" "$CONFIG" "${START_BLOCK:-}" "${END_BLOCK:-}" "$RUNNER"
}

# Calculate watch interval based on predefined test or block count.
# Longer runs poll less frequently to avoid GitHub API rate limits.
calculate_watch_interval() {
    # Check predefined test first
    if [[ -n "${TEST:-}" && -n "${TESTS[$TEST]:-}" ]]; then
        get_test_poll_interval "$TEST"
        return
    fi
    
    # Calculate from block range
    local start="${START_BLOCK:-}"
    local end="${END_BLOCK:-}"
    if [[ "$start" =~ ^[0-9]+$ && "$end" =~ ^[0-9]+$ && "$end" -gt "$start" ]]; then
        local blocks=$((end - start))
        if ((blocks < 1000000)); then
            echo 30    # < 1M blocks: 30s
        elif ((blocks < 5000000)); then
            echo 60    # 1M-5M blocks: 60s
        elif ((blocks < 10000000)); then
            echo 300   # 5M-10M blocks: 5 min
        else
            echo 600   # > 10M blocks: 10 min
        fi
    else
        echo 60  # Default
    fi
}

# GitHub's API can return transient errors (502/503, 403 rate limits) during
# long-running jobs. We retry gh run watch with exponential backoff, then fall
# back to polling gh run view for final status. This prevents false failures
# when the actual benchmark succeeds but API monitoring fails.
wait_for_completion() {
    local run_id="$1"
    local interval=$(calculate_watch_interval)
    log "Waiting for run $run_id (updates every ${interval}s)"
    log "https://github.com/${AVALANCHEGO_REPO}/actions/runs/${run_id}"
    
    # Retry gh run watch with exponential backoff on transient API errors
    local max_retries=3
    local delay=5
    for ((attempt=1; attempt<=max_retries; attempt++)); do
        if gh run watch "$run_id" --repo "$AVALANCHEGO_REPO" --interval "$interval" --exit-status; then
            return 0
        fi
        local exit_code=$?
        
        if ((attempt < max_retries)); then
            log "gh run watch failed (attempt $attempt/$max_retries), retrying in ${delay}s..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done
    
    # Fallback: poll gh run view for final status
    log "gh run watch exhausted retries, checking final status..."
    local conclusion
    for ((i=0; i<10; i++)); do
        conclusion=$(gh run view "$run_id" --repo "$AVALANCHEGO_REPO" --json status,conclusion \
            --jq 'if .status == "completed" then .conclusion else "pending" end') || {
            sleep 5
            continue
        }
        
        case "$conclusion" in
            success)
                log "Run completed successfully"
                return 0
                ;;
            pending)
                log "Run still in progress, waiting..."
                sleep 30
                ;;
            *)
                err "Run failed with conclusion: $conclusion"
                return 1
                ;;
        esac
    done
    
    err "Could not determine run status after retries"
    return 1
}

download_artifact() {
    local run_id="$1"
    local output_file="$DOWNLOAD_DIR/benchmark-output.json"
    
    log "Downloading artifact..."
    mkdir -p "$DOWNLOAD_DIR"
    gh run download "$run_id" \
        --repo "$AVALANCHEGO_REPO" \
        --pattern "benchmark-output-*" \
        --dir "$DOWNLOAD_DIR"
    
    # Flatten: gh extracts to $DOWNLOAD_DIR/<artifact-name>/benchmark-output.json
    find "$DOWNLOAD_DIR" -name "benchmark-output.json" -type f -exec mv {} "$output_file" \;
    find "$DOWNLOAD_DIR" -mindepth 1 -type d -delete 2>/dev/null || true
    
    [[ -f "$output_file" ]] || { err "No benchmark results found"; exit 1; }
    log "Results: $output_file"
}

check_status() {
    local run_id="$1"
    echo "https://github.com/${AVALANCHEGO_REPO}/actions/runs/${run_id}"
    echo -n "status: "
    gh run view "$run_id" --repo "$AVALANCHEGO_REPO" --json status,conclusion \
        --jq '.status + " (" + (.conclusion // "in progress") + ")"'
}

list_runs() {
    gh run list --repo "$AVALANCHEGO_REPO" --workflow "$WORKFLOW_NAME" --limit 10
}

list_tests() {
    for test in "${!TESTS[@]}"; do
        [[ "$test" == firewood* ]] && printf "  %-25s %s\n" "$test" "$(get_test_description "$test")"
    done | sort
}

cmd_trigger() {
    require_gh
    
    local test="${1:-${TEST:-}}"
    
    trigger_workflow "$test"  # Sets TRIGGER_RUN_ID on success
    
    if [[ -z "$TRIGGER_RUN_ID" ]]; then
        err "No run ID returned"
        exit 1
    fi
    local run_id="$TRIGGER_RUN_ID"
    log "Run ID: $run_id"
    
    wait_for_completion "$run_id"
    download_artifact "$run_id"
    log "Done"
}

cmd_status() {
    [[ -z "${1:-}" ]] && { err "usage: $0 status <run_id>"; exit 1; }
    require_gh
    check_status "$1"
}

cmd_list() {
    require_gh
    list_runs
}

cmd_tests() {
    list_tests
}

main() {
    local cmd="${1:-help}"
    shift || true
    
    case "$cmd" in
        trigger)        cmd_trigger "$@" ;;
        status)         cmd_status "$@" ;;
        list)           cmd_list "$@" ;;
        tests)          cmd_tests "$@" ;;
        help|-h|--help) cmd_help ;;
        *)              err "unknown command: $cmd"; cmd_help; exit 1 ;;
    esac
}

main "$@"
