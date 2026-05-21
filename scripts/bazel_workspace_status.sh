#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "${REPO_ROOT}/scripts/git_commit.sh"
echo "STABLE_GIT_COMMIT ${git_commit}"
echo "BUILD_TIMESTAMP $(date -u +%Y%m%d%H%M%S)"
