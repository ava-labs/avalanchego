#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
setup_task="${repo_root}/scripts/setup_task.sh"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

fixture_dir="${workdir}/fixture"
mkdir -p "${fixture_dir}/archive"
printf '#!/usr/bin/env bash\necho task\n' >"${fixture_dir}/archive/task"
chmod +x "${fixture_dir}/archive/task"
tar -czf "${fixture_dir}/task_linux_amd64.tar.gz" -C "${fixture_dir}/archive" task
checksum="$(sha256sum "${fixture_dir}/task_linux_amd64.tar.gz" | awk '{ print $1 }')"
printf '%s  task_linux_amd64.tar.gz\n' "${checksum}" >"${fixture_dir}/task_checksums.txt"

stub_dir="${workdir}/bin"
mkdir -p "${stub_dir}"
cat >"${stub_dir}/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

while (($#)); do
  case "$1" in
    --output)
      output="$2"
      shift 2
      ;;
    *) shift ;;
  esac
done

case "${output}" in
  *checksums*) cp "${TASK_FIXTURE}/task_checksums.txt" "${output}" ;;
  *) cp "${TASK_FIXTURE}/task_linux_amd64.tar.gz" "${output}" ;;
esac
EOF
chmod +x "${stub_dir}/curl"
cp "${stub_dir}/curl" "${stub_dir}/curl-ok"

run_download() {
  local runner_temp="$1"
  PATH="${stub_dir}:${PATH}" \
    TASK_FIXTURE="${fixture_dir}" \
    RUNNER_TEMP="${runner_temp}" \
    RUNNER_OS=Linux \
    RUNNER_ARCH=X64 \
    TASK_VERSION=v3.48.0 \
    "${setup_task}" download
}

version="$("${setup_task}" version)"
if [[ "${version}" != v3.48.0 ]]; then
  echo "expected pinned Task version v3.48.0, got ${version}" >&2
  exit 1
fi

runner_temp="${workdir}/runner-temp"
mkdir -p "${runner_temp}"
run_download "${runner_temp}"
task_path="${runner_temp}/task/v3.48.0/Linux-X64/task"
if [[ ! -x "${task_path}" ]]; then
  echo "expected Task archive to contain an executable task binary" >&2
  exit 1
fi

resolved_path="$(RUNNER_TEMP="${runner_temp}" RUNNER_OS=Linux RUNNER_ARCH=X64 TASK_VERSION=v3.48.0 "${setup_task}" path)"
if [[ "${resolved_path}" != "${task_path%/task}" ]]; then
  echo "expected task path ${task_path%/task}, got ${resolved_path}" >&2
  exit 1
fi

# A cache miss in a later setup action must not download Task again.
cat >"${stub_dir}/curl" <<'EOF'
#!/usr/bin/env bash
echo "curl ran despite an existing Task binary" >&2
exit 1
EOF
chmod +x "${stub_dir}/curl"
run_download "${runner_temp}"

# Reject an archive that does not match the release checksum.
cp "${stub_dir}/curl-ok" "${stub_dir}/curl"
printf '%064d  task_linux_amd64.tar.gz\n' 0 >"${fixture_dir}/task_checksums.txt"
mkdir -p "${workdir}/bad-checksum"
if run_download "${workdir}/bad-checksum" >"${workdir}/stdout" 2>"${workdir}/stderr"; then
  echo "expected checksum mismatch to fail" >&2
  exit 1
fi
if ! grep -q "checksum verification failed" "${workdir}/stderr"; then
  echo "checksum mismatch did not print the expected error" >&2
  exit 1
fi

echo "setup_task tests passed"
