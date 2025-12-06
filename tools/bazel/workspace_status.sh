#!/bin/bash
# Workspace status script for Bazel
# Provides build metadata for version injection

# Stable status (cached, won't trigger rebuilds on change)
echo "STABLE_GIT_COMMIT $(git rev-parse HEAD 2>/dev/null || echo unknown)"
echo "STABLE_GIT_BRANCH $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"

# Volatile status (changes trigger rebuilds but only for rules that use them)
echo "BUILD_TIMESTAMP $(date -u +%Y-%m-%dT%H:%M:%SZ)"
