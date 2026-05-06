#!/usr/bin/env bash

# Set up packaging workflow environment: resolve tag and import GPG key.
#
# Called directly from workflow YAML (workflow-*.sh scripts are CI glue,
# not developer entrypoints, and are exempt from the run_task.sh policy).
#
# Writes key=value outputs to $GITHUB_OUTPUT when running in CI, or to
# stdout when running locally (for inspection / testing).
#
# Required env vars:
#   GITHUB_REF     - Git ref (set by GitHub Actions; locally: any ref string)
#   GITHUB_SHA     - Git commit SHA (set by GitHub Actions; locally: any SHA)
#
# Optional env vars:
#   GITHUB_OUTPUT   - Path to step output file (CI only; omit for stdout)
#   TAG_INPUT       - Explicit tag from workflow_dispatch (empty for auto-detect)
#   GPG_PRIVATE_KEY - GPG private key content (empty for unsigned PR builds)

set -euo pipefail

OUTPUT="${GITHUB_OUTPUT:-/dev/stdout}"

# ── Resolve tag ──────────────────────────────────────────────────

TAG_INPUT="${TAG_INPUT:-}"

if [[ -n "${TAG_INPUT}" ]]; then
    TAG="${TAG_INPUT}"
elif [[ "${GITHUB_REF}" == refs/tags/* ]]; then
    TAG="${GITHUB_REF/refs\/tags\//}"
else
    TAG="v0.0.0-pr.${GITHUB_SHA::8}"
fi

echo "tag=${TAG}" >> "${OUTPUT}"
echo "Resolved tag: ${TAG}" >&2

# ── Import GPG key ───────────────────────────────────────────────

GPG_PRIVATE_KEY="${GPG_PRIVATE_KEY:-}"

if [[ -n "${GPG_PRIVATE_KEY}" ]]; then
    GPG_KEY_FILE="$(mktemp)"
    chmod 600 "${GPG_KEY_FILE}"
    printf '%s' "${GPG_PRIVATE_KEY}" > "${GPG_KEY_FILE}"
    echo "gpg-key-file=${GPG_KEY_FILE}" >> "${OUTPUT}"
    echo "GPG key written to temporary file" >&2
else
    echo "gpg-key-file=" >> "${OUTPUT}"
fi
