#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
temp_dir="$(mktemp -d)"
readonly temp_dir
trap 'rm -rf "${temp_dir}"' EXIT

mkdir "${temp_dir}/bin"
cat > "${temp_dir}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\t%s\t%s\n' "${PWD}" "${GOWORK-}" "$*" >> "${GO_INVOCATIONS}"
EOF
chmod +x "${temp_dir}/bin/go"

GO_INVOCATIONS="${temp_dir}/invocations" \
PATH="${temp_dir}/bin:${PATH}" \
  "${repo_root}/scripts/download_go_dependencies.sh"

cat > "${temp_dir}/expected" <<EOF
${repo_root}		mod download all
${repo_root}	off	mod download all
${repo_root}/graft/coreth	off	mod download all
${repo_root}/graft/evm	off	mod download all
${repo_root}/graft/subnet-evm	off	mod download all
${repo_root}/tools/external	off	mod download all
${repo_root}	off	install github.com/ava-labs/libevm/cmd/abigen@v1.13.14-0.2.0.release
EOF

diff -u "${temp_dir}/expected" "${temp_dir}/invocations"
