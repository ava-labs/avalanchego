#!/usr/bin/env bash

# Verify that Task version synchronization regenerates the checked-in archive
# checksums. Use a local copy of setup_task.sh and a local checksum fixture so
# this test does not modify the worktree or depend on GitHub availability.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
setup_task_source="${repo_root}/scripts/setup_task.sh"
sync_task_checksums="${repo_root}/scripts/sync_task_checksums.sh"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
setup_task="${workdir}/setup_task.sh"
cp "${setup_task_source}" "${setup_task}"

checksums="${workdir}/task_checksums.txt"
cat >"${checksums}" <<'EOF'
9a2727b7b74821e1a0a1fb17c477a7e84b7f1bbc8c3ca3bf47ed9a9557c2b986  task_darwin_arm64.tar.gz
f4bfc4eef1b2557b262f3cc0a79976a421885cb7b9e71cfe75568a2ebe4d7ae5  task_linux_amd64.tar.gz
15a54fd45f706ce0f4c679c93cf04e02d72ca7223f157209201a1576008056d6  task_linux_arm64.tar.gz
EOF

perl -0pi -e 's/(v3\.48\.0:task_(?:darwin|linux)_(?:amd64|arm64)\.tar\.gz\) echo )[0-9a-f]{64}/$1 . ("0" x 64)/ge' "${setup_task}"
if ! grep -q 'echo 0\{64\}' "${setup_task}"; then
  echo "failed to replace Task archive checksums" >&2
  exit 1
fi

"${sync_task_checksums}" v3.48.0 "file://${checksums}" "${setup_task}"
if ! cmp -s "${setup_task_source}" "${setup_task}"; then
  echo "Task version synchronization did not restore the archive checksums" >&2
  exit 1
fi

echo "sync_task_version tests passed"
