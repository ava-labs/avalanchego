#!/usr/bin/env bash

# Extract changelog for a given version from RELEASES.md and output
# nfpm changelog YAML format. Falls back to a minimal entry with a
# release link on failure — changelog should never block the build.
#
# Usage: extract-changelog.sh <version> <releases-file> <output-file>
# Example: extract-changelog.sh 1.14.1 RELEASES.md /tmp/changelog.yml
#
# The version should NOT have a "v" prefix.

set -euo pipefail

version="${1:?Usage: extract-changelog.sh <version> <releases-file> <output-file>}"
releases_file="${2:?Usage: extract-changelog.sh <version> <releases-file> <output-file>}"
output_file="${3:?Usage: extract-changelog.sh <version> <releases-file> <output-file>}"

fallback() {
    echo "warning: $1, using fallback changelog entry" >&2
    cat > "${output_file}" <<EOF
---
- semver: ${version}
  date: $(date -u +%Y-%m-%dT%H:%M:%SZ)
  packager: Ava Labs <security@avalabs.org>
  changes:
    - note: "See https://github.com/ava-labs/avalanchego/releases/tag/v${version}"
EOF
}

if [[ ! -f "${releases_file}" ]]; then
    fallback "releases file not found: ${releases_file}"
    exit 0
fi

# Match either "## [vX.Y.Z]" (released) or "## Pending (vX.Y.Z)" (unreleased)
# Extract everything between this heading and the next ## heading.
section=$(awk -v ver="${version}" '
    BEGIN { found = 0 }
    /^## / {
        if (found) exit
        # Match ## [vVERSION] or ## Pending (vVERSION)
        if (index($0, "[v" ver "]") || index($0, "(v" ver ")")) {
            found = 1
            next
        }
    }
    found { print }
' "${releases_file}")

if [[ -z "${section}" ]]; then
    fallback "version ${version} not found in ${releases_file}"
    exit 0
fi

# Convert markdown section to nfpm changelog YAML.
# Captures all bullet points (including indented sub-items), skips
# bare labels like "Added:", "Deprecated:", strips markdown backticks,
# and omits the "What's Changed" subsection (usually just a link).
notes=$(echo "${section}" | awk '
    /^### What'\''s Changed/ { skip = 1; next }
    /^### / { skip = 0; next }
    skip { next }
    /^\s*- / {
        line = $0
        sub(/^\s*- /, "", line)
        # Skip bare labels (e.g., "Added:", "Deprecated:")
        if (line ~ /^[A-Za-z]+:$/) next
        # Strip markdown backticks
        gsub(/`/, "", line)
        print line
    }
' || true)

if [[ -z "${notes}" ]]; then
    fallback "no changelog entries found for version ${version}"
    exit 0
fi

{
    echo "---"
    echo "- semver: ${version}"
    echo "  date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "  packager: Ava Labs <security@avalabs.org>"
    echo "  changes:"
    echo "${notes}" | while IFS= read -r note; do
        # Escape double quotes for YAML
        escaped="${note//\"/\\\"}"
        echo "    - note: \"${escaped}\""
    done
} > "${output_file}"

echo "Changelog extracted for version ${version}"
