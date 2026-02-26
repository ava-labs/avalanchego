#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck disable=SC1091 # git_commit.sh resolved at runtime via REPO_ROOT
source "${REPO_ROOT}/scripts/git_commit.sh"
# shellcheck disable=SC2154 # git_commit is defined in git_commit.sh
echo "STABLE_GIT_COMMIT ${git_commit}"
echo "BUILD_TIMESTAMP $(date -u +%Y%m%d%H%M%S)"
