#!/usr/bin/env bash
# release-check-exists.sh — fail loudly if a GitHub release already exists for <TAG>.
#
# Usage: release-check-exists.sh <TAG>
#
# Exit codes:
#   0  — release does NOT exist (proceed)
#   1  — release exists (refuse to overwrite)
#   2  — could not determine (auth, rate limit, network, outage, etc.) — fail closed
#
# Requires the `gh` CLI authenticated. Either GH_REPO or GITHUB_REPOSITORY
# must be set (the GitHub Actions runner sets GITHUB_REPOSITORY automatically).

set -euo pipefail

TAG="${1:?Usage: release-check-exists.sh <TAG>}"

REPO="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
if [[ -z "$REPO" ]]; then
  echo "Error: GH_REPO or GITHUB_REPOSITORY must be set" >&2
  exit 2
fi

tmp_out="$(mktemp)"
tmp_err="$(mktemp)"
trap 'rm -f "$tmp_out" "$tmp_err"' EXIT

# Prove the repo is actually reachable before trusting any "release missing"
# result. GitHub returns 404 for several distinct cases — release missing
# (good), repo missing/wrong (bad), repo private + token lacks access (bad) —
# and we can't distinguish them from the release output alone. Querying
# repos/${REPO} first removes the ambiguity: success here means "repo exists
# and is visible to this token".
repo_rc=0
gh api --include "repos/${REPO}" >"$tmp_out" 2>"$tmp_err" || repo_rc=$?
if [[ "$repo_rc" != "0" ]]; then
  repo_status="$(awk 'NR==1 && /^HTTP\// {print $2; exit}' "$tmp_out" || true)"
  echo "Error: cannot access repo '${REPO}' (HTTP ${repo_status:-unknown})." >&2
  echo "Refusing to proceed — release existence cannot be determined without repo access." >&2
  echo "--- stdout ---" >&2
  cat "$tmp_out" >&2
  echo "--- stderr ---" >&2
  cat "$tmp_err" >&2
  exit 2
fi

# The workflow creates releases AS DRAFTS by default. GitHub's
# `GET /repos/.../releases/tags/{tag}` endpoint deliberately excludes
# drafts — it returns 404 for any tag that has only a draft release.
# Using that endpoint would let us silently create a SECOND draft for the
# same tag on a re-trigger, breaking idempotence. The listing endpoint
# (`GET /repos/.../releases`) DOES include drafts (for users with
# collaborator access), so we use it with a tag-name filter to catch
# both draft and published releases.
list_rc=0
gh api --paginate "repos/${REPO}/releases?per_page=100" \
    --jq ".[] | select(.tag_name == \"${TAG}\") | {id: .id, draft: .draft, name: .name}" \
    >"$tmp_out" 2>"$tmp_err" || list_rc=$?

if [[ "$list_rc" != "0" ]]; then
  echo "Error: failed to list releases on '${REPO}'." >&2
  echo "Refusing to proceed (fail-closed):" >&2
  echo "--- stdout ---" >&2
  cat "$tmp_out" >&2
  echo "--- stderr ---" >&2
  cat "$tmp_err" >&2
  exit 2
fi

# gh api --jq prints one JSON object per matching release. Empty file = none.
if [[ -s "$tmp_out" ]]; then
  is_draft="$(jq -r '.draft' "$tmp_out" 2>/dev/null | head -n1 || echo unknown)"
  existing_id="$(jq -r '.id' "$tmp_out" 2>/dev/null | head -n1)"
  echo "Error: release for tag '${TAG}' already exists on GitHub (draft=${is_draft}, id=${existing_id})." >&2
  echo "Refusing to overwrite. To re-create, inspect and delete via the listing endpoint" >&2
  echo "(tag-keyed 'gh release view'/'gh release delete' may not see drafts depending on gh CLI version):" >&2
  echo "" >&2
  echo "  # Inspect:" >&2
  echo "  gh api --paginate \"repos/${REPO}/releases?per_page=100\" \\" >&2
  echo "    --jq \".[] | select(.tag_name == \\\"${TAG}\\\") | {id, draft, prerelease, name, assets: [.assets[].name]}\"" >&2
  echo "" >&2
  echo "  # Delete (by release id — works for drafts and published alike):" >&2
  echo "  gh api -X DELETE \"repos/${REPO}/releases/${existing_id}\"" >&2
  echo "" >&2
  echo "  # Then delete the tag separately (the release DELETE doesn't remove the underlying tag):" >&2
  echo "  git push origin --delete \"${TAG}\"" >&2
  exit 1
fi

echo "No existing release (draft or published) for ${TAG} — proceeding."
exit 0
