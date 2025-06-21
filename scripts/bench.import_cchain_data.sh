#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

if [ $# -ne 3 ]; then
    echo "Error: incorrect number of arguments provided $#"
    echo "Usage: $0 <current-state-dir-dst> <block-source-src> <current-state-dir-src>"
    echo "Example: $0 /path/to/current-state-dst /path/to/block-source-src /path/to/current-state-src"
    echo "Expected resulting file structure:"
    echo "<current-state-dir-dst>/"
    echo "├── blocks/                   # Copied from <block-source-src>"
    echo "└── current-state/       # Copied from <current-state-dir-src>"
    exit 1
fi

function import_cchain_data() {
    local current_state_dir_arg=$1
    local source_block_dir_arg=$2
    local current_state_dir_src_arg=$3

    local source_block_dir_dst="${current_state_dir_arg}/blocks"
    local current_state_dir_dst="${current_state_dir_arg}/current-state"

    echo "Copying block source db from $source_block_dir_arg to $source_block_dir_dst"
    "$SCRIPT_DIR/copy_dir.sh" "$source_block_dir_arg" "$source_block_dir_dst"
    
    echo "Copying current state directory from $current_state_dir_src_arg to $current_state_dir_dst"
    "$SCRIPT_DIR/copy_dir.sh" "$current_state_dir_src_arg" "$current_state_dir_dst"
}

import_cchain_data $@
