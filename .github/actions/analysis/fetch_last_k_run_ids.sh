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

    # Get workflow runs with proper gh CLI syntax
    local runs_json=$(gh run list \
        --repo "$REPO" \
        --status completed \
        --limit 50 \
        --json databaseId,number,status,conclusion,jobs)

    # Process each run
    echo "$runs_json" | jq -r '.[] | @base64' | while IFS= read -r encoded_run && [ $found_count -lt $BASELINE_COUNT ]; do
        if [ -z "$encoded_run" ]; then
            continue
        fi

        local run=$(echo "$encoded_run" | base64 -d)
        local run_id=$(echo "$run" | jq -r '.databaseId')
        local run_number=$(echo "$run" | jq -r '.number')
        local conclusion=$(echo "$run" | jq -r '.conclusion')

        # Skip if not successful
        if [ "$conclusion" != "success" ]; then
            continue
        fi

        # Explicitly exclude current run
        if [ "$run_id" = "$CURRENT_RUN_ID" ]; then
            continue
        fi

        # Check if this run has our target job that succeeded
        local job_found=$(echo "$run" | jq -r --arg job_name "$JOB_ID" '
            .jobs[]? | select(.name == $job_name and .conclusion == "success") | .name
        ')

        if [ -n "$job_found" ] && [ "$job_found" != "null" ]; then
            echo "${run_id}:${run_number}"
            ((found_count++))
        fi
    done
}

# Create JSON configuration
create_config() {
    local baseline_runs=("$@")

    # Calculate current run timing
    local current_start=$(date +%s)000  # Convert to milliseconds
    local current_end=$((current_start + MONITORING_PERIOD * 1000))

    # Build baselines JSON array
    local baselines_json=""

    if [ ${#baseline_runs[@]} -gt 0 ]; then
        local first=true

        for run_info in "${baseline_runs[@]}"; do
            IFS=':' read -r run_id run_number <<< "$run_info"

            if [ "$first" = true ]; then
                first=false
            else
                baselines_json+=","
            fi

            baselines_json+=$(cat << EOF

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
    }
EOF
)
        done
    fi

    # Create complete JSON config
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
  "baselines": [${baselines_json}
  ],
  "output_file": "metric_visualization_${CURRENT_RUN_ID}.html"
}
EOF
}

# Main execution
main() {
    local baseline_runs

    # Get baseline runs as array
    mapfile -t baseline_runs < <(get_baseline_run_ids)

    if ! create_config "${baseline_runs[@]}"; then
        echo "ERROR: Failed to create configuration file" >&2
        echo "::error::Failed to create configuration file"
        exit 1
    fi

    # Success case - show what we found
    if [ ${#baseline_runs[@]} -eq 0 ]; then
        echo "Found 0 baseline runs for visualization (excluding current run ${CURRENT_RUN_ID})"
    else
        echo "Found ${#baseline_runs[@]} baseline runs for visualization (excluding current run ${CURRENT_RUN_ID})"
    fi
}

main "$@"
