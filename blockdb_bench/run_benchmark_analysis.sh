#!/bin/bash

set -e

# Configuration with defaults
NUM_RUNS=${NUM_RUNS:-2}
RUNNER_NAME=${RUNNER_NAME:-dev}
START_BLOCK=${START_BLOCK:-10000001}
END_BLOCK=${END_BLOCK:-11000000}
METRICS_ENABLED=${METRICS_ENABLED:-true}
BLOCKDB_ONLY=${BLOCKDB_ONLY:-false}
GRAFANA_BASE_URL=${GRAFANA_BASE_URL:-"https://grafana-poc.avax-dev.network/d/R8N89hznk2/c-chain-blockdb"}
GRAFANA_ORG_ID=${GRAFANA_ORG_ID:-1}
GRAFANA_REFRESH=${GRAFANA_REFRESH:-"10s"}

# Paths
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "${SCRIPT_DIR}/.." && pwd )"
SUMMARY_DIR="${SCRIPT_DIR}/summary"
CURRENT_STATE_DIR="${PROJECT_ROOT}/tmp/blockdb-test/state"
BLOCK_DIR="${PROJECT_ROOT}/tmp/reexecute-cchain/10m/blocks"
BENCHMARK_SCRIPT="${PROJECT_ROOT}/scripts/benchmark_cchain_range.sh"

echo "========================================="
echo "Starting Benchmark Analysis"
echo "========================================="
echo "Configuration:"
echo "  Number of run sets: ${NUM_RUNS}"
echo "  Runner name: ${RUNNER_NAME}"
echo "  Start block: ${START_BLOCK}"
echo "  End block: ${END_BLOCK}"
echo "  Metrics enabled: ${METRICS_ENABLED}"
echo "  BlockDB only: ${BLOCKDB_ONLY}"
echo "========================================="

# Source environment variables if .envrc.local exists
if [ -f "${PROJECT_ROOT}/.envrc.local" ]; then
    echo "Sourcing .envrc.local..."
    source "${PROJECT_ROOT}/.envrc.local"
else
    echo "Warning: .envrc.local not found, continuing without it..."
fi

# Create required directories for prometheus metrics
echo "Setting up prometheus directories..."
mkdir -p ~/.tmpnet/prometheus/file_sd_configs

# Clean and create summary directory
echo "Preparing summary directory..."
rm -rf "${SUMMARY_DIR}"
mkdir -p "${SUMMARY_DIR}"

# Arrays to store metrics for aggregation
declare -a CONTROL_MGAS_S=()
declare -a BLOCKDB_MGAS_S=()

# Function to clean state
clean_state() {
    local config=$1  # "control" or "blockdb"
    echo "Cleaning state for ${config} configuration..."

    # If state directory exists, remove it with force
    if [ -d "${PROJECT_ROOT}/tmp/blockdb-test/state" ]; then
        # Fix permissions first to ensure we can delete everything
        chmod -R u+w "${PROJECT_ROOT}/tmp/blockdb-test/state" 2>/dev/null || true

        # Remove contents first, then directory
        find "${PROJECT_ROOT}/tmp/blockdb-test/state" -mindepth 1 -delete 2>/dev/null || {
            # Fallback: try removing with rm -rf after fixing permissions
            rm -rf "${PROJECT_ROOT}/tmp/blockdb-test/state"/*
            rm -rf "${PROJECT_ROOT}/tmp/blockdb-test/state"
        }
    fi

    # Create fresh directory and copy state based on configuration
    mkdir -p "${PROJECT_ROOT}/tmp/blockdb-test/state"

    if [ "$config" == "blockdb" ]; then
        echo "Copying state from state-blockdb directory..."
        cp -r "${PROJECT_ROOT}/tmp/reexecute-cchain/10m/state-control/"* "${PROJECT_ROOT}/tmp/blockdb-test/state/"
    else
        echo "Copying state from state-control directory..."
        cp -r "${PROJECT_ROOT}/tmp/reexecute-cchain/10m/state-control/"* "${PROJECT_ROOT}/tmp/blockdb-test/state/"
    fi

    # Sleep for a few seconds to ensure copy operations are fully completed
    echo "Waiting for copy operations to complete..."
    sleep 3
}

# Function to get folder size in human-readable format
get_folder_size() {
    local folder=$1
    if [ -d "$folder" ]; then
        du -sh "$folder" 2>/dev/null | cut -f1
    else
        echo "N/A"
    fi
}

# Function to format duration in human readable format
format_duration() {
    local seconds=$1
    local hours=$((seconds / 3600))
    local minutes=$(((seconds % 3600) / 60))
    local secs=$((seconds % 60))

    if [ $hours -gt 0 ]; then
        printf "%dh %dm %ds" $hours $minutes $secs
    elif [ $minutes -gt 0 ]; then
        printf "%dm %ds" $minutes $secs
    else
        printf "%ds" $secs
    fi
}

# Function to parse metrics from benchmark output
parse_metrics() {
    local output_file=$1
    # Look for the benchmark result line with mgas/s metric
    grep -E "BenchmarkReexecuteRange.*mgas/s" "$output_file" | tail -1
}

# Function to extract numeric metrics from benchmark line
extract_metrics() {
    local metrics_line=$1
    local config=$2

    if [ -z "$metrics_line" ]; then
        return 1
    fi

    # Parse the mgas/s field (second-to-last field, before the unit)
    local mgas_s=$(echo "$metrics_line" | awk '{print $(NF-1)}')

    # Store in appropriate arrays
    if [ "$config" == "control" ]; then
        CONTROL_MGAS_S+=("$mgas_s")
    else
        BLOCKDB_MGAS_S+=("$mgas_s")
    fi
}

# Function to calculate average
calc_avg() {
    local sum=0
    local count=0
    for val in "$@"; do
        sum=$(awk "BEGIN {print $sum + $val}")
        ((count++))
    done
    if [ $count -eq 0 ]; then
        echo "N/A"
    else
        awk "BEGIN {printf \"%.2f\", $sum / $count}"
    fi
}

# Function to calculate min
calc_min() {
    local min=""
    for val in "$@"; do
        if [ -z "$min" ]; then
            min=$val
        else
            min=$(awk "BEGIN {print ($val < $min) ? $val : $min}")
        fi
    done
    echo "${min:-N/A}"
}

# Function to calculate max
calc_max() {
    local max=""
    for val in "$@"; do
        if [ -z "$max" ]; then
            max=$val
        else
            max=$(awk "BEGIN {print ($val > $max) ? $val : $max}")
        fi
    done
    echo "${max:-N/A}"
}

# Function to calculate median
calc_median() {
    local count=$#
    if [ $count -eq 0 ]; then
        echo "N/A"
        return
    fi

    local sorted=($(printf '%s\n' "$@" | sort -n))

    if [ $((count % 2)) -eq 0 ]; then
        local mid1=${sorted[$((count/2-1))]}
        local mid2=${sorted[$((count/2))]}
        awk "BEGIN {printf \"%.2f\", ($mid1 + $mid2) / 2}"
    else
        echo "${sorted[$((count/2))]}"
    fi
}

# Function to calculate percentage difference
# For throughput (higher is better): positive = better, negative = worse
calc_percentage_diff() {
    local control=$1
    local blockdb=$2

    if [ -z "$control" ] || [ -z "$blockdb" ] || [ "$control" == "N/A" ] || [ "$blockdb" == "N/A" ]; then
        echo "N/A"
        return
    fi

    awk "BEGIN {printf \"%.2f\", (($blockdb - $control) / $control) * 100}"
}

# Function to calculate percentage difference for timing metrics
# For timing (lower is better): positive = better, negative = worse
# Inverted formula so that when blockdb is slower (larger value), result is negative
calc_percentage_diff_timing() {
    local control=$1
    local blockdb=$2

    if [ -z "$control" ] || [ -z "$blockdb" ] || [ "$control" == "N/A" ] || [ "$blockdb" == "N/A" ]; then
        echo "N/A"
        return
    fi

    awk "BEGIN {printf \"%.2f\", (($control - $blockdb) / $control) * 100}"
}

# Function to generate Grafana dashboard URL for a benchmark run
generate_grafana_url() {
    local run_num=$1
    local config=$2
    local start_time=$3
    local end_time=$4
    local run_id=$5

    # Add buffer time (1 minute before and after)
    local buffer=60
    local from_timestamp=$((start_time - buffer))
    local to_timestamp=$((end_time + buffer))

    # Convert to milliseconds (Grafana expects milliseconds)
    from_timestamp="${from_timestamp}000"
    to_timestamp="${to_timestamp}000"

    # Build the URL with filters
    # %7C = |, %3D = =, %2C = ,
    local url="${GRAFANA_BASE_URL}?orgId=${GRAFANA_ORG_ID}&refresh=${GRAFANA_REFRESH}"
    url="${url}&var-filter=runner%7C%3D%7C${RUNNER_NAME}"
    # Config filter removed for both BlockDB and control runs
    url="${url}&var-filter=run_number%7C%3D%7C${run_num}"
    url="${url}&var-filter=run_id%7C%3D%7C${run_id}"
    url="${url}&from=${from_timestamp}&to=${to_timestamp}"

    echo "$url"
}

# Function to run a single benchmark
run_benchmark() {
    local run_num=$1
    local config=$2  # "control" or "blockdb"
    local output_file=$3
    local start_time_var=$4  # Variable name to store start time
    local end_time_var=$5    # Variable name to store end time
    local run_id_var=$6      # Variable name to store run ID

    echo ""
    echo "----------------------------------------"
    echo "Run ${run_num}: ${config}"
    echo "----------------------------------------"

    # Clean state before each run
    clean_state "$config"

    # Set up environment variables
    export CURRENT_STATE_DIR
    export BLOCK_DIR
    export RUNNER_NAME
    export START_BLOCK
    export END_BLOCK
    # Generate unique 6-character ID for this run
    local run_id=$(openssl rand -hex 3)
    export LABELS="run_number=${run_num},config=${config},run_id=${run_id}"

    # Store run_id in the provided variable name
    eval "$run_id_var=$run_id"
    export METRICS_ENABLED
    # We handle output redirection ourselves, don't use BENCHMARK_OUTPUT_FILE
    unset BENCHMARK_OUTPUT_FILE

    # Add CONFIG for blockdb run
    if [ "$config" == "blockdb" ]; then
        export CONFIG="blockdb"
    else
        unset CONFIG
    fi

    echo "Running benchmark with labels: ${LABELS}"
    echo "Run ID: ${run_id}"
    echo "Output will be saved to: ${output_file}"

    # Capture start timestamp (seconds since epoch)
    local start_ts=$(date +%s)

    # Run the benchmark and redirect all output to the file only
    # Filter out linker warnings that pollute the logs
    # This prevents benchmark logs from appearing in the main script output
    bash "${BENCHMARK_SCRIPT}" 2>&1 | grep -v "^ld: warning: object file" > "${output_file}"

    # Capture end timestamp
    local end_ts=$(date +%s)

    # Store timestamps in the provided variable names
    eval "$start_time_var=$start_ts"
    eval "$end_time_var=$end_ts"

    # Parse and display metrics
    local metrics_line=$(parse_metrics "${output_file}")
    if [ -n "$metrics_line" ]; then
        local mgas_s=$(echo "$metrics_line" | awk '{print $(NF-1)}')

        echo ""
        echo "Benchmark completed. Results:"
        echo "┌──────────────────────────────────────────────┐"
        printf "│ %-44s │\n" "  Throughput:       ${mgas_s} mgas/s"
        echo "└──────────────────────────────────────────────┘"
    else
        echo "ERROR: Could not parse metrics from benchmark output"
    fi
}

# Initialize consolidated summary file
BENCHMARK_SUMMARY_FILE="${SUMMARY_DIR}/benchmark_summary.txt"

# Track overall start time
OVERALL_START_TIME=$(date +%s)
OVERALL_START_DATE=$(date "+%Y-%m-%d %H:%M:%S")

cat > "${BENCHMARK_SUMMARY_FILE}" <<EOF
=========================================
BENCHMARK SUMMARY - ALL RUNS
=========================================
Configuration:
  Runner: ${RUNNER_NAME}
  Block Range: ${START_BLOCK} - ${END_BLOCK}
  Number of run sets: ${NUM_RUNS}
  Started: ${OVERALL_START_DATE}

EOF

# Main loop
for run in $(seq 1 ${NUM_RUNS}); do
    echo ""
    echo "========================================="
    echo "Starting Run Set ${run}/${NUM_RUNS}"
    echo "========================================="

    # Track run set start time
    RUN_SET_START_TIME=$(date +%s)

    # Create run directory
    RUN_DIR="${SUMMARY_DIR}/run_${run}"
    mkdir -p "${RUN_DIR}"

    # Run blockdb benchmark first
    BLOCKDB_OUTPUT="${RUN_DIR}/benchmark_blockdb_output.txt"
    run_benchmark ${run} "blockdb" "${BLOCKDB_OUTPUT}" BLOCKDB_START_TIME BLOCKDB_END_TIME BLOCKDB_RUN_ID

    # Calculate BlockDB run duration
    BLOCKDB_DURATION=$((BLOCKDB_END_TIME - BLOCKDB_START_TIME))
    BLOCKDB_DURATION_FORMATTED=$(format_duration $BLOCKDB_DURATION)

    # Generate Grafana URL for blockdb run
    BLOCKDB_GRAFANA_URL=$(generate_grafana_url ${run} "blockdb" ${BLOCKDB_START_TIME} ${BLOCKDB_END_TIME} ${BLOCKDB_RUN_ID})

    # Get database sizes for blockdb
    DB_SIZE_BLOCKDB=$(get_folder_size "${PROJECT_ROOT}/tmp/blockdb-test/state/db")
    CHAINDB_SIZE=$(get_folder_size "${PROJECT_ROOT}/tmp/blockdb-test/state/chain-data-dir")

    # Parse metrics from blockdb run
    BLOCKDB_METRICS=$(parse_metrics "${BLOCKDB_OUTPUT}")

    # Extract and store metrics for aggregation
    extract_metrics "$BLOCKDB_METRICS" "blockdb"

    # Extract individual metrics for display
    BLOCKDB_THROUGHPUT=$(echo "$BLOCKDB_METRICS" | awk '{print $(NF-1)}')

    # Write blockdb results immediately to summary
    cat >> "${BENCHMARK_SUMMARY_FILE}" <<EOF
=========================================
Run Set ${run}
=========================================

BlockDB (Run ID: ${BLOCKDB_RUN_ID}):
  Throughput (mgas/s)      : ${BLOCKDB_THROUGHPUT}
  ─────────────────────────────────────────
  DB Size (leveldb)        : ${DB_SIZE_BLOCKDB}
  DB Size (chaindb)        : ${CHAINDB_SIZE}
  Run Duration             : ${BLOCKDB_DURATION_FORMATTED}
  Grafana Dashboard        : ${BLOCKDB_GRAFANA_URL}

EOF

    # Run control benchmark (without blockdb) second - only if not in blockdb-only mode
    if [ "$BLOCKDB_ONLY" != "true" ]; then
        CONTROL_OUTPUT="${RUN_DIR}/benchmark_control_output.txt"
        run_benchmark ${run} "control" "${CONTROL_OUTPUT}" CONTROL_START_TIME CONTROL_END_TIME CONTROL_RUN_ID

        # Calculate Control run duration
        CONTROL_DURATION=$((CONTROL_END_TIME - CONTROL_START_TIME))
        CONTROL_DURATION_FORMATTED=$(format_duration $CONTROL_DURATION)

        # Generate Grafana URL for control run
        CONTROL_GRAFANA_URL=$(generate_grafana_url ${run} "control" ${CONTROL_START_TIME} ${CONTROL_END_TIME} ${CONTROL_RUN_ID})

        # Get database sizes for control
        DB_SIZE=$(get_folder_size "${PROJECT_ROOT}/tmp/blockdb-test/state/db")

        # Parse metrics from control run
        CONTROL_METRICS=$(parse_metrics "${CONTROL_OUTPUT}")

        # Extract and store metrics for aggregation
        extract_metrics "$CONTROL_METRICS" "control"

        # Extract individual metrics for display
        CONTROL_THROUGHPUT=$(echo "$CONTROL_METRICS" | awk '{print $(NF-1)}')

        # Calculate throughput comparison for this run
        RUN_THROUGHPUT_DIFF=$(calc_percentage_diff "$CONTROL_THROUGHPUT" "$BLOCKDB_THROUGHPUT")
        THROUGHPUT_STATUS="⚠ Same"
        if [ "$RUN_THROUGHPUT_DIFF" != "N/A" ]; then
            if [ $(awk "BEGIN {print ($RUN_THROUGHPUT_DIFF > 0)}") -eq 1 ]; then
                THROUGHPUT_STATUS="✓ BlockDB ${RUN_THROUGHPUT_DIFF}% faster"
            elif [ $(awk "BEGIN {print ($RUN_THROUGHPUT_DIFF < 0)}") -eq 1 ]; then
                THROUGHPUT_STATUS="✗ BlockDB slower (${RUN_THROUGHPUT_DIFF}%)"
            fi
        fi
    else
        echo "Skipping control run (BLOCKDB_ONLY=true)"
        CONTROL_THROUGHPUT="N/A"
        CONTROL_GRAFANA_URL="N/A"
        CONTROL_DURATION_FORMATTED="N/A"
        DB_SIZE="N/A"
        THROUGHPUT_STATUS="N/A (BlockDB only mode)"
    fi

    # Calculate run set duration
    RUN_SET_END_TIME=$(date +%s)
    RUN_SET_DURATION=$((RUN_SET_END_TIME - RUN_SET_START_TIME))
    RUN_SET_DURATION_FORMATTED=$(format_duration $RUN_SET_DURATION)

    # Write control results immediately to summary with brief comparison
    if [ "$BLOCKDB_ONLY" != "true" ]; then
        cat >> "${BENCHMARK_SUMMARY_FILE}" <<EOF
CONTROL (No BlockDB) (Run ID: ${CONTROL_RUN_ID}):
  Throughput (mgas/s)      : ${CONTROL_THROUGHPUT}
  ─────────────────────────────────────────
  DB Size (leveldb)        : ${DB_SIZE}
  Run Duration             : ${CONTROL_DURATION_FORMATTED}
  Grafana Dashboard        : ${CONTROL_GRAFANA_URL}

Throughput Comparison: ${THROUGHPUT_STATUS}
Run Set Duration: ${RUN_SET_DURATION_FORMATTED}

EOF
    else
        cat >> "${BENCHMARK_SUMMARY_FILE}" <<EOF
CONTROL (No BlockDB): SKIPPED (BlockDB only mode)

Run Set Duration: ${RUN_SET_DURATION_FORMATTED}

EOF
    fi

    echo "Run set ${run} completed in ${RUN_SET_DURATION_FORMATTED}. Results saved to ${RUN_DIR}"
done

echo ""
echo "========================================="
echo "All benchmark runs completed!"
echo "========================================="

# Calculate overall duration
OVERALL_END_TIME=$(date +%s)
OVERALL_END_DATE=$(date "+%Y-%m-%d %H:%M:%S")
OVERALL_DURATION=$((OVERALL_END_TIME - OVERALL_START_TIME))
OVERALL_DURATION_FORMATTED=$(format_duration $OVERALL_DURATION)

# Append overall timing to benchmark summary
cat >> "${BENCHMARK_SUMMARY_FILE}" <<EOF
=========================================
OVERALL SUMMARY
=========================================
Started:  ${OVERALL_START_DATE}
Ended:    ${OVERALL_END_DATE}
Total Duration: ${OVERALL_DURATION_FORMATTED}

EOF

echo "Total benchmark duration: ${OVERALL_DURATION_FORMATTED}"

# Generate results summary
RESULTS_FILE="${SUMMARY_DIR}/RESULTS.txt"

# Calculate statistics
if [ "$BLOCKDB_ONLY" != "true" ]; then
    CONTROL_THROUGHPUT_AVG=$(calc_avg "${CONTROL_MGAS_S[@]}")
    CONTROL_THROUGHPUT_MIN=$(calc_min "${CONTROL_MGAS_S[@]}")
    CONTROL_THROUGHPUT_MAX=$(calc_max "${CONTROL_MGAS_S[@]}")
    CONTROL_THROUGHPUT_MED=$(calc_median "${CONTROL_MGAS_S[@]}")
else
    CONTROL_THROUGHPUT_AVG="N/A"
    CONTROL_THROUGHPUT_MIN="N/A"
    CONTROL_THROUGHPUT_MAX="N/A"
    CONTROL_THROUGHPUT_MED="N/A"
fi

BLOCKDB_THROUGHPUT_AVG=$(calc_avg "${BLOCKDB_MGAS_S[@]}")
BLOCKDB_THROUGHPUT_MIN=$(calc_min "${BLOCKDB_MGAS_S[@]}")
BLOCKDB_THROUGHPUT_MAX=$(calc_max "${BLOCKDB_MGAS_S[@]}")
BLOCKDB_THROUGHPUT_MED=$(calc_median "${BLOCKDB_MGAS_S[@]}")

# Calculate percentage differences using MEDIAN values
# For throughput (higher is better): use standard calculation
if [ "$BLOCKDB_ONLY" != "true" ]; then
    DIFF_THROUGHPUT=$(calc_percentage_diff "$CONTROL_THROUGHPUT_MED" "$BLOCKDB_THROUGHPUT_MED")
else
    DIFF_THROUGHPUT="N/A"
fi

cat > "${RESULTS_FILE}" <<EOF
=========================================
BENCHMARK RESULTS SUMMARY
=========================================
Generated: $(date "+%Y-%m-%d %H:%M:%S")
Configuration:
  Number of run sets: ${NUM_RUNS}
  Runner: ${RUNNER_NAME}
  Block Range: ${START_BLOCK} - ${END_BLOCK}
  BlockDB Only Mode: ${BLOCKDB_ONLY}

Timing:
  Started:  ${OVERALL_START_DATE}
  Ended:    ${OVERALL_END_DATE}
  Total Duration: ${OVERALL_DURATION_FORMATTED}

=========================================
STATISTICS SUMMARY
=========================================

EOF

if [ "$BLOCKDB_ONLY" != "true" ]; then
    cat >> "${RESULTS_FILE}" <<EOF
Control (No BlockDB):
  Throughput (mgas/s)      : Avg=${CONTROL_THROUGHPUT_AVG}  Med=${CONTROL_THROUGHPUT_MED}  Min=${CONTROL_THROUGHPUT_MIN}  Max=${CONTROL_THROUGHPUT_MAX}

EOF
else
    cat >> "${RESULTS_FILE}" <<EOF
Control (No BlockDB): SKIPPED (BlockDB only mode)

EOF
fi

cat >> "${RESULTS_FILE}" <<EOF
BlockDB:
  Throughput (mgas/s)      : Avg=${BLOCKDB_THROUGHPUT_AVG}  Med=${BLOCKDB_THROUGHPUT_MED}  Min=${BLOCKDB_THROUGHPUT_MIN}  Max=${BLOCKDB_THROUGHPUT_MAX}

EOF

if [ "$BLOCKDB_ONLY" != "true" ]; then
    cat >> "${RESULTS_FILE}" <<EOF
=========================================
PERFORMANCE COMPARISON (based on MEDIAN values)
=========================================

BlockDB vs Control - Percentage Difference
Note: All percentages normalized so positive = better, negative = worse

KEY METRICS:

  Throughput (mgas/s)      : ${DIFF_THROUGHPUT}%
    $([ $(awk "BEGIN {print ($DIFF_THROUGHPUT > 0)}") -eq 1 ] && echo "✓ BlockDB is FASTER" || ([ $(awk "BEGIN {print ($DIFF_THROUGHPUT < 0)}") -eq 1 ] && echo "✗ BlockDB is SLOWER" || echo "⚠ Same performance"))

INTERPRETATION:
  - Positive % = BlockDB performs BETTER than control
  - Negative % = BlockDB performs WORSE than control
  - All comparisons use MEDIAN values for robustness against outliers

EOF
else
    cat >> "${RESULTS_FILE}" <<EOF
=========================================
PERFORMANCE COMPARISON
=========================================

No comparison available (BlockDB only mode - no control runs performed)

EOF
fi

cat >> "${RESULTS_FILE}" <<EOF
=========================================
END OF RESULTS SUMMARY
=========================================
EOF

echo "Generating results summary..."
echo ""
echo "Results are available in: ${SUMMARY_DIR}"
echo ""
echo "Summary files:"
echo "  All runs: ${BENCHMARK_SUMMARY_FILE}"
echo "  Results: ${RESULTS_FILE}"
echo ""
for run in $(seq 1 ${NUM_RUNS}); do
    echo "  Run ${run} outputs: ${SUMMARY_DIR}/run_${run}/"
done
