#!/usr/bin/env bash

# Set up packaging workflow environment: resolve tag and import GPG key.
#
# Called directly from workflow YAML (workflow-*.sh scripts are CI glue,
# not developer entrypoints, and are exempt from the run_task.sh policy).
#
# Writes key=value outputs to $GITHUB_OUTPUT when running in CI, or to
# stdout when running locally (for inspection / testing).

set -euo pipefail

: "${GITHUB_REF:?GITHUB_REF must be set (Git ref, e.g. refs/tags/v1.14.1; locally: any ref string)}"
: "${GITHUB_SHA:?GITHUB_SHA must be set (Git commit SHA; locally: any SHA)}"

# Optional env vars (defaulted below):
#   GITHUB_OUTPUT   - step-output file path; empty/unset writes to stdout
#   TAG_INPUT       - explicit tag from workflow_dispatch (empty: auto-detect)
#   GPG_PRIVATE_KEY - signing key content (empty: ephemeral PR/local signing)
#   RELEASE         - non-empty marks a release build (missing key is then
#                     fatal instead of falling back to ephemeral signing)
OUTPUT="${GITHUB_OUTPUT:-/dev/stdout}"

# ── Resolve tag ──────────────────────────────────────────────────

TAG_INPUT="${TAG_INPUT:-}"

if [[ -n "${TAG_INPUT}" ]]; then
    # Second layer behind the workflow's tag-existence gate: reject a malformed
    # dispatch input before it reaches the signing step. The strong "is this an
    # actual tag" check lives in build-linux-packages.yml (needs origin/network).
    if [[ ! "${TAG_INPUT}" =~ ^v[0-9] ]]; then
        echo "ERROR: TAG_INPUT '${TAG_INPUT}' must start with 'v<digit>' (e.g. v1.14.1)" >&2
        exit 1
    fi
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
RELEASE="${RELEASE:-}"

if [[ -n "${GPG_PRIVATE_KEY}" ]]; then
    GPG_KEY_FILE="$(mktemp)"
    chmod 600 "${GPG_KEY_FILE}"
    printf '%s' "${GPG_PRIVATE_KEY}" > "${GPG_KEY_FILE}"
    echo "gpg-key-file=${GPG_KEY_FILE}" >> "${OUTPUT}"
    echo "GPG key written to temporary file" >&2
elif [[ -n "${RELEASE}" ]]; then
    echo "ERROR: release build requires GPG_PRIVATE_KEY but none was provided" >&2
    exit 1
else
    echo "gpg-key-file=" >> "${OUTPUT}"
fi
