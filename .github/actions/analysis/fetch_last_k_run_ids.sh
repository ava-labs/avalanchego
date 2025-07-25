#!/bin/bash

set -euo pipefail

# Configuration from environment or defaults
BASELINE_COUNT="${BASELINE_COUNT:-3}"
MONITORING_PERIOD="${MONITORING_PERIOD:-900}"
CURRENT_RUN_ID="${GITHUB_RUN_ID}"
CURRENT_RUN_ATTEMPT="${GITHUB_RUN_ATTEMPT:-1}"
JOB_ID="${GITHUB_JOB}"
REPO="${GITHUB_REPOSITORY}"
WORKFLOW_NAME="${GITHUB_WORKFLOW}"
QUERY="${QUERY}"
METRIC_NAME="${METRIC_NAME}"
X_AXIS_LABEL="${X_AXIS_LABEL:-Time}"
Y_AXIS_LABEL="${Y_AXIS_LABEL:-$METRIC_NAME}"

# Get baseline run IDs using gh CLI, excluding current run
get_baseline_run_ids() {
    local found_count=0

    # Get successful workflow runs from the same workflow
    local runs_json=$(gh run list \
        --repo "$REPO" \
        --workflow "$WORKFLOW_NAME" \
        --status completed \
        --limit 50 \
        --json databaseId,number,conclusion)

    echo "DEBUG: Found runs for workflow '$WORKFLOW_NAME':" >&2
    echo "$runs_json" | jq -r '.[] | "\(.number): \(.conclusion) (ID: \(.databaseId))"' >&2

    # Process each successful run
    echo "$runs_json" | jq -r '.[] | select(.conclusion == "success") | "\(.databaseId):\(.number)"' | \
    while IFS=':' read -r run_id run_number && [ $found_count -lt $BASELINE_COUNT ]; do
        # Explicitly exclude current run
        if [ "$run_id" = "$CURRENT_RUN_ID" ]; then
            echo "DEBUG: Skipping current run $run_id" >&2
            continue
        fi

        echo "DEBUG: Checking jobs for run $run_id (run #$run_number)" >&2

        # Get jobs for this specific run
        local jobs_json=$(gh run view "$run_id" --repo "$REPO" --json jobs 2>/dev/null || echo '{"jobs":[]}')

        echo "DEBUG: Jobs for run $run_id:" >&2
        echo "$jobs_json" | jq -r '.jobs[] | "\(.name): \(.conclusion)"' >&2

        # Check if this run has our target job that succeeded
        local job_found=$(echo "$jobs_json" | jq -r --arg job_name "$JOB_ID" '
            .jobs[]? | select(.name == $job_name and .conclusion == "success") | .name
        ')

        if [ -n "$job_found" ] && [ "$job_found" != "null" ]; then
            echo "DEBUG: Found matching job '$job_found' in run $run_id" >&2
            echo "${run_id}:${run_number}"
            ((found_count++))
        else
            echo "DEBUG: No matching job '$JOB_ID' found in run $run_id" >&2
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

    if [ ${#baseline_runs[@]} -gt 0 ] && [ -n "${baseline_runs[0]}" ]; then
        local first=true

        for run_info in "${baseline_runs[@]}"; do
            if [ -z "$run_info" ]; then
                continue
            fi

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
    echo "DEBUG: Looking for baselines in workflow '$WORKFLOW_NAME' with job '$JOB_ID'" >&2

    local baseline_runs

    # Get baseline runs as array
    mapfile -t baseline_runs < <(get_baseline_run_ids)

    if ! create_config "${baseline_runs[@]}"; then
        echo "ERROR: Failed to create configuration file" >&2
        echo "::error::Failed to create configuration file"
        exit 1
    fi

    # Success case - show what we found
    if [ ${#baseline_runs[@]} -eq 0 ] || [ -z "${baseline_runs[0]}" ]; then
        echo "Found 0 baseline runs for workflow '$WORKFLOW_NAME' job '$JOB_ID' (excluding current run ${CURRENT_RUN_ID})"
    else
        echo "Found ${#baseline_runs[@]} baseline runs for workflow '$WORKFLOW_NAME' job '$JOB_ID' (excluding current run ${CURRENT_RUN_ID})"
    fi
}

main "$@"
