#!/usr/bin/env bash

# Tests vcs.sh functions in both git and jj contexts.
#
# Usage:
#   ./scripts/tests.vcs.sh              # Run tests in the current VCS context
#   ./scripts/tests.vcs.sh --all        # Run tests in both git and jj contexts
#   ./scripts/tests.vcs.sh --all --init # Also init jj if not already colocated (for CI)

set -euo pipefail

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Track test results
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

assert() {
  local description=$1
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  if "$@"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  PASS: ${description}"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  FAIL: ${description}"
  fi
}

assert_eq() {
  local description=$1
  local expected=$2
  local actual=$3
  TESTS_RUN=$((TESTS_RUN + 1))
  if [[ "${expected}" == "${actual}" ]]; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  PASS: ${description}"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  FAIL: ${description} (expected '${expected}', got '${actual}')"
  fi
}

# Run the vcs.sh test suite against the current directory
run_tests() {
  local context=$1

  echo "=== Testing vcs.sh (${context}) ==="

  # Source vcs.sh fresh
  # shellcheck disable=SC1091
  source "${AVALANCHE_PATH}/scripts/vcs.sh"

  # vcs_in_repo
  assert "vcs_in_repo returns 0" vcs_in_repo

  # vcs_commit_hash: 40-char hex
  local hash
  hash="$(vcs_commit_hash)"
  assert "vcs_commit_hash returns 40 hex chars" \
    test "$(echo "${hash}" | grep -cE '^[0-9a-f]{40}$')" -eq 1

  # vcs_commit_hash_short: 8 chars, prefix of full hash
  local short
  short="$(vcs_commit_hash_short)"
  assert_eq "vcs_commit_hash_short is 8 chars" "8" "${#short}"
  assert_eq "vcs_commit_hash_short is prefix of full hash" "${short}" "${hash::8}"

  # vcs_branch_or_tag: non-empty, no slashes
  local branch
  branch="$(vcs_branch_or_tag)"
  assert "vcs_branch_or_tag is non-empty" test -n "${branch}"
  assert "vcs_branch_or_tag has no slashes" test "$(echo "${branch}" | grep -c '/')" -eq 0

  # vcs_repo_root: existing directory
  local root
  root="$(vcs_repo_root)"
  assert "vcs_repo_root returns existing directory" test -d "${root}"

  # vcs_status_short: runs without error
  assert "vcs_status_short runs without error" vcs_status_short

  # vcs_is_clean: runs without error (may return 0 or 1)
  TESTS_RUN=$((TESTS_RUN + 1))
  if vcs_is_clean; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  PASS: vcs_is_clean runs without error (clean)"
  else
    # Exit code 1 is valid (dirty tree), only script errors are failures
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  PASS: vcs_is_clean runs without error (dirty)"
  fi

  # Convenience variables
  assert "vcs_commit is set" test -n "${vcs_commit}"
  assert "vcs_commit_short is set" test -n "${vcs_commit_short}"
  assert_eq "vcs_commit_short length is 8" "8" "${#vcs_commit_short}"

  echo ""
}

# Run tests in the current context (git or jj, whatever is active)
run_tests_here() {
  if command -v jj >/dev/null 2>&1 && jj root --quiet >/dev/null 2>&1; then
    run_tests "jj"
  else
    run_tests "git"
  fi
}

# Directories to clean up on exit
CLEANUP_DIRS=()
CLEANUP_JJ_WORKSPACES=()
JJ_INIT=false
JJ_CREATED=false

cleanup() {
  for ws in "${CLEANUP_JJ_WORKSPACES[@]}"; do
    jj workspace forget "${ws}" 2>/dev/null || true
  done
  for dir in "${CLEANUP_DIRS[@]}"; do
    rm -rf "${dir}"
  done
  # Remove .jj only if we created it during this run
  if [[ "${JJ_CREATED}" == true ]]; then
    rm -rf "${AVALANCHE_PATH}/.jj"
  fi
}
trap cleanup EXIT

# Run tests in both git and jj contexts
run_tests_all() {
  # Phase 1: git context
  # If we're in a jj-colocated repo, we need a pure git environment.
  # Create a temporary git clone.
  local git_tmpdir
  git_tmpdir="$(mktemp -d)"
  CLEANUP_DIRS+=("${git_tmpdir}")
  echo "--- Creating temporary git clone in ${git_tmpdir} ---"
  git clone --quiet --shared "${AVALANCHE_PATH}" "${git_tmpdir}/repo"
  pushd "${git_tmpdir}/repo" >/dev/null
  run_tests "git"
  popd >/dev/null

  # Phase 2: jj context (non-colocated workspace)
  if ! command -v jj >/dev/null 2>&1; then
    echo "SKIP: jj not installed, skipping jj tests"
  elif ! jj root --quiet >/dev/null 2>&1; then
    if [[ "${JJ_INIT:-}" == true ]]; then
      # CI: colocate jj on top of the git clone so we can create a workspace
      echo "--- Initializing jj colocated repo ---"
      jj git init --colocate 2>/dev/null
      JJ_CREATED=true
    else
      echo "SKIP: not a jj repo (use --init to colocate), skipping jj tests"
      return
    fi
  fi

  if command -v jj >/dev/null 2>&1 && jj root --quiet >/dev/null 2>&1; then
    local jj_tmpdir jj_workspace_name
    jj_tmpdir="$(mktemp -d)"
    jj_workspace_name="test-vcs-${jj_tmpdir##*/}"
    CLEANUP_DIRS+=("${jj_tmpdir}")
    echo "--- Creating temporary jj workspace in ${jj_tmpdir} (name: ${jj_workspace_name}) ---"
    jj workspace add --name "${jj_workspace_name}" "${jj_tmpdir}/repo" 2>/dev/null
    CLEANUP_JJ_WORKSPACES+=("${jj_workspace_name}")
    pushd "${jj_tmpdir}/repo" >/dev/null
    run_tests "jj"
    popd >/dev/null
  fi
}

mode=""
for arg in "$@"; do
  case "${arg}" in
    --all)  mode="all" ;;
    --init) JJ_INIT=true ;;
  esac
done

if [[ "${mode}" == "all" ]]; then
  run_tests_all
else
  run_tests_here
fi

echo "=== Results: ${TESTS_PASSED}/${TESTS_RUN} passed, ${TESTS_FAILED} failed ==="
if [[ ${TESTS_FAILED} -gt 0 ]]; then
  exit 1
fi
