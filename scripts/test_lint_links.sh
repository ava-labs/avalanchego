#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_dir="${repo_root}/scripts/testdata/lint_links"
work_dir="$(mktemp -d)"
server_pid=""

cleanup() {
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" 2>/dev/null || true
    wait "${server_pid}" 2>/dev/null || true
  fi
  rm -rf "${work_dir}"
}
trap cleanup EXIT

go run "${fixture_dir}/http_server.go" "${work_dir}/server-url" &
server_pid=$!

for _ in {1..100}; do
  if [[ -s "${work_dir}/server-url" ]]; then
    break
  fi
  sleep 0.1
done

if [[ ! -s "${work_dir}/server-url" ]]; then
  echo "local HTTP server did not start" >&2
  exit 1
fi

server_url="$(<"${work_dir}/server-url")"
cat > "${work_dir}/target.md" <<'EOF'
# Target

## Anchor
EOF
cat > "${work_dir}/valid.md" <<EOF
[local file](target.md)
[local anchor](target.md#anchor)
[HTTP URL](${server_url}/ok)
[excluded localhost URL](http://localhost:1/not-running)
EOF

"${repo_root}/scripts/lint_links.sh" "${work_dir}/valid.md"

printf '[missing local file](missing.md)\n' > "${work_dir}/broken.md"
if "${repo_root}/scripts/lint_links.sh" "${work_dir}/broken.md"; then
  echo "link check accepted a broken local file link" >&2
  exit 1
fi

printf '[missing anchor](target.md#missing)\n' > "${work_dir}/broken-anchor.md"
if "${repo_root}/scripts/lint_links.sh" "${work_dir}/broken-anchor.md"; then
  echo "link check accepted a broken anchor" >&2
  exit 1
fi

echo "link check fixtures passed"
