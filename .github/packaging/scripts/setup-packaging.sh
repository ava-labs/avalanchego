#!/usr/bin/env bash

# Set up packaging workflow environment: resolve tag and import GPG key.
#
# Called from workflow YAML after the packaging overlay has been applied.
# Writes outputs to $GITHUB_OUTPUT for use by subsequent workflow steps.
#
# Required env vars (provided by GitHub Actions runner):
#   GITHUB_OUTPUT  - Path to step output file
#   GITHUB_REF     - Git ref that triggered the workflow
#   GITHUB_SHA     - Git commit SHA
#
# Optional env vars:
#   TAG_INPUT       - Explicit tag from workflow_dispatch (empty for auto-detect)
#   GPG_PRIVATE_KEY - GPG private key content (empty for unsigned PR builds)

set -euo pipefail

# ── Resolve tag ──────────────────────────────────────────────────

TAG_INPUT="${TAG_INPUT:-}"

if [[ -n "${TAG_INPUT}" ]]; then
    TAG="${TAG_INPUT}"
elif [[ "${GITHUB_REF}" == refs/tags/* ]]; then
    TAG="${GITHUB_REF/refs\/tags\//}"
else
    TAG="v0.0.0-pr.${GITHUB_SHA::8}"
fi

echo "tag=${TAG}" >> "${GITHUB_OUTPUT}"
echo "Resolved tag: ${TAG}"

# ── Import GPG key ───────────────────────────────────────────────

GPG_PRIVATE_KEY="${GPG_PRIVATE_KEY:-}"

if [[ -n "${GPG_PRIVATE_KEY}" ]]; then
    GPG_KEY_FILE="$(mktemp)"
    chmod 600 "${GPG_KEY_FILE}"
    printf '%s' "${GPG_PRIVATE_KEY}" > "${GPG_KEY_FILE}"
    echo "gpg-key-file=${GPG_KEY_FILE}" >> "${GITHUB_OUTPUT}"
    echo "GPG key written to temporary file"
else
    echo "gpg-key-file=" >> "${GITHUB_OUTPUT}"
fi
