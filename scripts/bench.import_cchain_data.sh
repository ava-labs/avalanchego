#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

if [ $# -ne 4 ]; then
    echo "Error: incorrect number of arguments provided $#"
    echo "Usage: $0 <execution-data-directory> <source-db-directory> <execution-db-directory> <execution-chain-data-directory>"
    echo "Example: $0 /path/to/execution/data /path/to/source/db /path/to/execution-db /path/to/chain-data"
    exit 1
fi

function import_cchain_data() {
    local execution_data_dir_arg=$1
    local source_db_dir_arg=$2
    local db_dir_arg=$3
    local chain_data_dir_arg=$4

    local source_db_dir_dst="${execution_data_dir_arg}/blocks"
    local db_dir_dst="${execution_data_dir_arg}/db"
    local chain_data_dir_dst="${execution_data_dir_arg}/chain-data-dir"

    
    echo "Copying block source db from $source_db_dir_arg to $source_db_dir_dst"
    "$SCRIPT_DIR/copy_dir.sh" "$source_db_dir_arg" "$source_db_dir_dst"
    
    echo "Copying db directory from $db_dir_arg to $db_dir_dst"
    "$SCRIPT_DIR/copy_dir.sh" "$db_dir_arg" "$db_dir_dst"

    echo "Copying chain data directory from $chain_data_dir_arg to $chain_data_dir_dst"
    "$SCRIPT_DIR/copy_dir.sh" "$chain_data_dir_arg" "$chain_data_dir_dst"
}

import_cchain_data $@

# ./scripts/bench.import_cchain_data.sh $(mktemp -d) 's3://statesync-testing/blocks-mainnet-200/*' 's3://statesync-testing/coreth-state-100-firewood/vmdb/*' 's3://statesync-testing/coreth-state-100-firewood/chain-data-dir'