#!/usr/bin/env bash

# Provides VCS-agnostic functions for use in build scripts. Supports both
# git and jj ([Jujutsu](https://docs.jj-vcs.dev/)) so that local
# development works from either VCS.  CI stays git-only since GitHub
# Actions clones via git.
#
# Why jj? It offers better ergonomics for common workflows like stacking
# changes, rebasing, and managing multiple working copies. However, jj
# workspaces lack a .git directory, so build scripts that call git
# directly fail. This file encapsulates VCS operations behind an
# abstraction layer so callers don't need to know which VCS is in use.
#
# Since this file only defines functions and variables, it is intended to
# be sourced rather than executed.

# Ignore warnings about variables appearing unused since this file is not
# the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Returns true if running inside a jj repository. Falls back to git if jj
# is not installed or the current directory is not inside a jj repo.
_vcs_is_jj() { command -v jj >/dev/null 2>&1 && jj root --quiet >/dev/null 2>&1; }

# Full commit hash of the current revision.
vcs_commit_hash() {
  if _vcs_is_jj; then
    jj log --no-graph -r @ -T 'commit_id' 2>/dev/null
  else
    git rev-parse HEAD
  fi
}

# First 8 characters of the commit hash.
vcs_commit_hash_short() {
  local hash
  hash="$(vcs_commit_hash)"
  echo "${hash::8}"
}

# Current branch, bookmark, or tag name suitable for use as a Docker image
# tag. Falls back to "ci_dummy" when no name can be determined. Slashes
# are replaced with dashes since they are not legal in Docker image tags.
vcs_branch_or_tag() {
  local name=""
  if _vcs_is_jj; then
    # Prefer the first bookmark pointing at the current revision
    name="$(jj log --no-graph -r @ -T 'bookmarks.map(|b| b.name()).join(",")' 2>/dev/null)"
    # Take the first bookmark if multiple exist
    name="${name%%,*}"
  else
    name="$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true)"
  fi

  if [[ -z "${name}" ]]; then
    name=ci_dummy
  elif [[ "${name}" == */* ]]; then
    name="$(echo "${name}" | tr '/' '-')"
  fi
  echo "${name}"
}

# Repository root directory.
vcs_repo_root() {
  if _vcs_is_jj; then
    jj root 2>/dev/null
  else
    git rev-parse --show-toplevel
  fi
}

# Exits 0 if the working copy is clean, 1 otherwise.
vcs_is_clean() {
  if _vcs_is_jj; then
    # jj considers a revision "empty" when there are no changes
    local empty
    empty="$(jj log --no-graph -r @ -T 'empty' 2>/dev/null)"
    [[ "${empty}" == "true" ]]
  else
    test -z "$(git status --porcelain)"
  fi
}

# Show changed files (short summary).
vcs_status_short() {
  if _vcs_is_jj; then
    jj diff --summary 2>/dev/null
  else
    git status --short
  fi
}

# Returns 0 if inside a VCS repository (jj or git).
vcs_in_repo() {
  if _vcs_is_jj; then
    return 0
  fi
  git rev-parse --is-inside-work-tree >/dev/null 2>&1
}

# --- Convenience variables for use by callers ---
# WARNING: these use the most recent commit even if there are un-committed changes present
vcs_commit="${AVALANCHEGO_COMMIT:-$(vcs_commit_hash)}"
vcs_commit_short="${vcs_commit::8}"
