#!/bin/bash
set -uo pipefail

# Suppress exit code 2 during early startup to avoid false
# "No unexpected container exits" failures in Antithesis.
#
# The graceful SIGTERM handler is registered in app.Run.
# If the fault injector stops a container before app.Run is reached —
# during Go init, instrumentation (InitializeModule), or early main —
# Go's default signal disposition exits with code 2. We treat that as
# success within a short grace period.

GRACE_PERIOD=3
start=$(date +%s)

./avalanchego "$@"
exit_code=$?

elapsed=$(( $(date +%s) - start ))
if [ "$exit_code" -eq 2 ] && [ "$elapsed" -lt "$GRACE_PERIOD" ]; then
    exit 0
fi

exit "$exit_code"
