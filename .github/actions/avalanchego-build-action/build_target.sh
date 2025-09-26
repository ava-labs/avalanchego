#!/usr/bin/env bash
# Build AvalancheGo target binary and optionally execute with arguments
# Usage: ./scripts/build_target.sh <target> [args...]

set -euo pipefail

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 <target> [args...]"
    echo "Valid targets: avalanchego, reexecution"
    exit 1
fi

TARGET="$1"
shift # Remove target from arguments, remaining args are for execution

case "$TARGET" in
    "avalanchego")
        if [[ ! -f "./scripts/run_task.sh" || ! -x "./scripts/run_task.sh" ]]; then
            echo "Error: ./scripts/run_task.sh not found or not executable"
            exit 1
        fi
        ./scripts/run_task.sh build
        EXECUTABLE="./build/avalanchego"

        if [[ ! -f "$EXECUTABLE" ]]; then
            echo "Error: Binary $EXECUTABLE was not created"
            exit 1
        fi

        echo "BINARY_PATH=$PWD/$EXECUTABLE"

        if [[ $# -gt 0 ]]; then
            "$EXECUTABLE" "$@"
        fi
        ;;
    "reexecution")
        # Compile reexecution benchmark test into binary
        go test -c github.com/ava-labs/avalanchego/tests/reexecute/c -o reexecute-benchmark
        EXECUTABLE="./reexecute-benchmark"

        if [[ ! -f "$EXECUTABLE" ]]; then
            echo "Error: Binary $EXECUTABLE was not created"
            exit 1
        fi

        echo "BINARY_PATH=$PWD/$EXECUTABLE"
        ;;
    *)
        echo "Error: Invalid target '$TARGET'. Valid targets: avalanchego, reexecution"
        exit 1
        ;;
esac
