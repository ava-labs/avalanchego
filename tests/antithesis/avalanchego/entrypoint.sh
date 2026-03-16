#!/bin/bash
set -uo pipefail

# Grace period (in seconds) during which exit code 2 is suppressed.
# When the Antithesis fault injector stops a container shortly after
# startup, the process may still be in Go's init phase (before main()
# is reached). Go's default SIGTERM handler exits with code 2 in that
# case, which trips the "No unexpected container exits" property.
GRACE_PERIOD=3
start=$(date +%s)

./avalanchego "$@"
exit_code=$?

elapsed=$(( $(date +%s) - start ))
if [ "$exit_code" -eq 2 ] && [ "$elapsed" -lt "$GRACE_PERIOD" ]; then
    exit 0
fi

exit "$exit_code"
