#!/bin/bash
set -uo pipefail

# Antithesis can send SIGTERM within ~100ms of startup, before app.Run
# registers the graceful signal handler. Go's default disposition exits
# with code 2, which Antithesis flags as an unexpected container exit.
#
# To handle this, bash traps SIGTERM and exits 0 during the grace period.
# After the grace period, the trap is replaced to forward
# SIGTERM to the child so graceful shutdown works normally.

readonly GRACE_PERIOD=1

trap 'exit 0' TERM

./avalanchego "$@" &
child_pid=$!

sleep "$GRACE_PERIOD" &
sleep_pid=$!

wait -n "$child_pid" "$sleep_pid"

if kill -0 "$child_pid" 2>/dev/null; then
    # grace period expired, avalanchego still running
    trap 'kill -TERM "$child_pid"' TERM
    wait "$child_pid"
    exit $?
else
    # avalanchego exited during grace period
    kill "$sleep_pid" 2>/dev/null
    wait "$sleep_pid" 2>/dev/null
    wait "$child_pid"
    exit $?
fi
