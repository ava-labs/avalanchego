#!/bin/bash
# Workspace status script for Bazel build stamping.
# Provides build metadata for version injection via x_defs.
#
# Reference: https://ndumas.com/2024/05/stamping-builds-with-bazel/
#
# Usage: Configured in .bazelrc with:
#   build --workspace_status_command=.bazel/workspace_status.sh
#   build --stamp

set -euo pipefail

# Stable status (cached, won't trigger rebuilds unless value changes)
echo "STABLE_GIT_COMMIT $(git rev-parse HEAD 2>/dev/null || echo unknown)"
echo "STABLE_GIT_BRANCH $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"

# Volatile status (always changes, only rebuilds rules that use these values)
echo "BUILD_TIMESTAMP $(date -u +%Y-%m-%dT%H:%M:%SZ)"
