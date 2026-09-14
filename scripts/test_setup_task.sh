#!/usr/bin/env bash

set -euo pipefail

# Test Task setup without downloading a release or using a GitHub Actions runner.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SETUP_TASK="${REPO_ROOT}/scripts/setup_task.sh"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

# Create the release archive that the curl fixture returns.
FIXTURE_DIR="${WORKDIR}/fixture"
mkdir -p "${FIXTURE_DIR}/archive"
printf '#!/usr/bin/env bash\necho task\n' >"${FIXTURE_DIR}/archive/task"
chmod +x "${FIXTURE_DIR}/archive/task"
tar -czf "${FIXTURE_DIR}/task_linux_amd64.tar.gz" -C "${FIXTURE_DIR}/archive" task

STUB_DIR="${WORKDIR}/bin"
mkdir -p "${STUB_DIR}"
# Return the local archive instead of downloading from the Task release.
cat >"${STUB_DIR}/curl" <<'EOF'
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

cp "${TASK_FIXTURE}/task_linux_amd64.tar.gz" "${output}"
EOF
chmod +x "${STUB_DIR}/curl"
cp "${STUB_DIR}/curl" "${STUB_DIR}/curl-fixture"

# Control the archive hash so each test can exercise success or failure against
# the checked-in expected checksum.
cat >"${STUB_DIR}/shasum" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

echo "${TASK_SHASUM_RESULT:?TASK_SHASUM_RESULT must be set}  $3"
EOF
chmod +x "${STUB_DIR}/shasum"

# Simulate the Linux amd64 environment that the setup action supplies.
run_download() {
  local runner_temp="$1"
  local checksum_result="${2:-${TASK_CHECKSUM}}"
  PATH="${STUB_DIR}:${PATH}" \
    TASK_FIXTURE="${FIXTURE_DIR}" \
    TASK_SHASUM_RESULT="${checksum_result}" \
    RUNNER_TEMP="${runner_temp}" \
    RUNNER_OS=Linux \
    RUNNER_ARCH=X64 \
    TASK_VERSION=v3.48.0 \
    "${SETUP_TASK}" download
}

# Read the Task version that CI pins in tools/external/go.mod.
VERSION="$("${SETUP_TASK}" version)"
if [[ "${VERSION}" != v3.48.0 ]]; then
  echo "expected pinned Task version v3.48.0, got ${VERSION}" >&2
  exit 1
fi

# Download and extract the release archive into the cache path.
TASK_CHECKSUM="$(TASK_VERSION=v3.48.0 "${SETUP_TASK}" checksum task_linux_amd64.tar.gz)"
RUNNER_TEMP="${WORKDIR}/runner-temp"
mkdir -p "${RUNNER_TEMP}"
run_download "${RUNNER_TEMP}"
TASK_PATH="${RUNNER_TEMP}/task/v3.48.0/Linux-X64/task"
if [[ ! -x "${TASK_PATH}" ]]; then
  echo "expected Task archive to contain an executable task binary" >&2
  exit 1
fi

# Return the directory that contains the extracted Task binary.
RESOLVED_PATH="$(RUNNER_TEMP="${RUNNER_TEMP}" RUNNER_OS=Linux RUNNER_ARCH=X64 TASK_VERSION=v3.48.0 "${SETUP_TASK}" path)"
if [[ "${RESOLVED_PATH}" != "${TASK_PATH%/task}" ]]; then
  echo "expected task path ${TASK_PATH%/task}, got ${RESOLVED_PATH}" >&2
  exit 1
fi

# A cache hit in a later setup action must not download Task again.
cat >"${STUB_DIR}/curl" <<'EOF'
#!/usr/bin/env bash
echo "curl ran despite an existing Task binary" >&2
exit 1
EOF
chmod +x "${STUB_DIR}/curl"
run_download "${RUNNER_TEMP}"

# Reject an archive that does not match the checked-in checksum.
cp "${STUB_DIR}/curl-fixture" "${STUB_DIR}/curl"
mkdir -p "${WORKDIR}/bad-checksum"
if run_download "${WORKDIR}/bad-checksum" "$(printf '%064d' 0)" >"${WORKDIR}/stdout" 2>"${WORKDIR}/stderr"; then
  echo "expected checksum mismatch to fail" >&2
  exit 1
fi
if ! grep -q "checksum verification failed" "${WORKDIR}/stderr"; then
  echo "checksum mismatch did not print the expected error" >&2
  exit 1
fi

# Reject a runner platform without a supported Task release mapping.
if RUNNER_TEMP="${WORKDIR}/unsupported" RUNNER_OS=macOS RUNNER_ARCH=X64 TASK_VERSION=v3.48.0 \
  "${SETUP_TASK}" download >"${WORKDIR}/stdout" 2>"${WORKDIR}/stderr"; then
  echo "expected unsupported platform to fail" >&2
  exit 1
fi
if ! grep -q "Unsupported Task platform: macOS-X64" "${WORKDIR}/stderr"; then
  echo "unsupported platform did not print the expected error" >&2
  exit 1
fi

echo "setup_task tests passed"
