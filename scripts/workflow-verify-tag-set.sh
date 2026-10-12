#!/usr/bin/env bash
# workflow-verify-tag-set.sh — CI-only glue for the release workflow.
#
# Usage: workflow-verify-tag-set.sh <TAG>
#
# Thin wrapper over verify_tags_remote.sh so the release workflow can invoke the
# shared multi-module tag verifier directly, without the run_task.sh toolchain
# (the release jobs run plain bash and have no task/nix/go setup). Named
# workflow-*.sh per scripts/actionlint.sh, which exempts that convention from
# the "workflow run: steps must call scripts/* via run_task.sh" check.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${HERE}/verify_tags_remote.sh" "$@"
