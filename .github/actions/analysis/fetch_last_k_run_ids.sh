#!/bin/bash

set -euo pipefail

# Configuration from environment or defaults
BASELINE_COUNT="${BASELINE_COUNT:-3}"
MONITORING_PERIOD="${MONITORING_PERIOD:-900}"
CURRENT_RUN_ID="${GITHUB_RUN_ID}"
CURRENT_RUN_ATTEMPT="${GITHUB_RUN_ATTEMPT:-1}"
JOB_ID="${GITHUB_JOB}"
REPO="${GITHUB_REPOSITORY}"
QUERY="${QUERY}"
METRIC_NAME="${METRIC_NAME}"
X_AXIS_LABEL="${X_AXIS_LABEL:-Time}"
Y_AXIS_LABEL="${Y_AXIS_LABEL:-$METRIC_NAME}"

# Get baseline run IDs using gh CLI, excluding current run
get_baseline_run_ids() {
    local baseline_runs=()
    local found_count=0

    # Get successful workflow runs and process them
    while IFS= read -r run && [ $found_count -lt $BASELINE_COUNT ]; do
        if [ -z "$run" ]; then
            continue
        fi

        local run_id=$(echo "$run" | jq -r '.databaseId')
        local run_number=$(echo "$run" | jq -r '.number')

        # Explicitly exclude current run
        if [ "$run_id" = "$CURRENT_RUN_ID" ]; then
            continue
        fi

        # Check if this run has our target job that succeeded
        local job_found=$(echo "$run" | jq -r --arg job_name "$JOB_ID" '
            .jobs[]? | select(.name == $job_name and .conclusion == "success") | .name
        ')

        if [ -n "$job_found" ]; then
            baseline_runs+=("${run_id}:${run_number}")
            ((found_count++))
        fi
    done < <(gh run list \
        --repo "$REPO" \
        --status completed \
        --conclusion success \
        --limit 50 \
        --json databaseId,number,jobs)

    echo "${baseline_runs[@]}"
}

# Create JSON configuration
create_config() {
    local baseline_runs=($1)

    if [ ${#baseline_runs[@]} -eq 0 ]; then
        echo "ERROR: No baseline runs found for job '${JOB_ID}' (excluding current run ${CURRENT_RUN_ID})" >&2
        echo "::error::No baseline runs found for job '${JOB_ID}'"
        exit 1
    fi

    # Calculate current run timing
    local current_start=$(date +%s)000  # Convert to milliseconds
    local current_end=$((current_start + MONITORING_PERIOD * 1000))

    # Start building JSON config
    cat > metric_config.json << EOF
{
  "query": "${QUERY}",
  "metric_name": "${METRIC_NAME}",
  "x_axis_label": "${X_AXIS_LABEL}",
  "y_axis_label": "${Y_AXIS_LABEL}",
  "candidate": {
    "start_time": ${current_start},
    "end_time": ${current_end},
    "name": "Current Run (#${CURRENT_RUN_ID})",
    "labels": {
      "gh_run_id": "${CURRENT_RUN_ID}",
      "gh_job_id": "${JOB_ID}",
      "gh_run_attempt": "${CURRENT_RUN_ATTEMPT}",
      "gh_repo": "${REPO}"
    }
  },
  "baselines": [
EOF

    # Add baseline runs
    local first=true
    for run_info in "${baseline_runs[@]}"; do
        IFS=':' read -r run_id run_number <<< "$run_info"

        if [ "$first" = true ]; then
            first=false
        else
            echo "," >> metric_config.json
        fi

        cat >> metric_config.json << EOF
    {
      "start_time": 0,
      "end_time": 0,
      "name": "Run ${run_number} (#${run_id})",
      "labels": {
        "gh_run_id": "${run_id}",
        "gh_job_id": "${JOB_ID}",
        "gh_run_attempt": "1",
        "gh_repo": "${REPO}"
      }
    }EOF
    done

    cat >> metric_config.json << EOF

  ],
  "output_file": "metric_visualization_${CURRENT_RUN_ID}.html"
}
EOF
}

# Main execution
main() {
    local baseline_runs

    if ! baseline_runs=$(get_baseline_run_ids); then
        echo "ERROR: Failed to fetch baseline runs from GitHub API" >&2
        echo "::error::Failed to fetch baseline runs from GitHub API"
        exit 1
    fi

    if [ -z "$baseline_runs" ]; then
        echo "ERROR: No baseline runs found for job '${JOB_ID}' (excluding current run ${CURRENT_RUN_ID})" >&2
        echo "::error::No baseline runs found for job '${JOB_ID}'"
        exit 1
    fi

    if ! create_config "$baseline_runs"; then
        echo "ERROR: Failed to create configuration file" >&2
        echo "::error::Failed to create configuration file"
        exit 1
    fi

    # Success case - show what we found
    local baseline_count=$(echo "$baseline_runs" | wc -w)
    echo "Found ${baseline_count} baseline runs for visualization (excluding current run ${CURRENT_RUN_ID})"
}

main "$@"
