#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config="${repo_root}/lychee.toml"

if [[ "$#" -eq 0 ]]; then
  set -- "${repo_root}"
fi

lychee --config "${config}" --root-dir "${repo_root}" "$@"

# GitHub returns 503 to Lychee's HTTP client. Use curl for GitHub URLs instead;
# it follows redirects and needs no authentication token.
while IFS= read -r url; do
  curl --fail --location --silent --show-error --max-time 20 --output /dev/null "${url}"
done < <(lychee --config "${config}" --dump "$@" | awk '/^https:\/\/github\.com\//' | sort -u)

# Remote documentation sites commonly render headings client-side, so their
# anchors cannot be checked from an HTTP response. Check anchors for local files,
# where Lychee can inspect the target document.
exec lychee --config "${config}" --root-dir "${repo_root}" --scheme file --include-fragments=anchor-only "$@"
