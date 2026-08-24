#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
temp_dir="$(mktemp -d)"
readonly temp_dir
trap 'rm -rf "${temp_dir}"' EXIT

while IFS= read -r go_mod; do
  module_directory="${go_mod#"${repo_root}/"}"
  module_directory="${module_directory%/go.mod}"
  if [[ "${module_directory}" == "go.mod" ]]; then
    module_directory="."
  fi
  printf '%s\n' "${module_directory}"
done < <(find "${repo_root}" -name go.mod -not -path '*/.git/*') \
  | sort > "${temp_dir}/expected-modules"

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

awk -F '\t' -v root="${repo_root}/" '{
  directory = $1
  if (directory == substr(root, 1, length(root) - 1)) {
    directory = "."
  } else {
    sub("^" root, "", directory)
  }
  print directory
  if ($2 != "off") {
    print "GOWORK was not disabled for " directory > "/dev/stderr"
    exit 1
  }
  if ($3 != "mod download all") {
    print "unexpected go command for " directory ": " $3 > "/dev/stderr"
    exit 1
  }
}' "${temp_dir}/invocations" | sort > "${temp_dir}/actual-modules"

diff -u "${temp_dir}/expected-modules" "${temp_dir}/actual-modules"
