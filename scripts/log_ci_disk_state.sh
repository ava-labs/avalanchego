#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF' >&2
Usage: ./scripts/log_ci_disk_state.sh [--group generic] [--group bazel] [--group docker]
EOF
}

groups=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --group)
      if [[ $# -lt 2 ]]; then
        usage
        exit 2
      fi
      case "$2" in
        generic|bazel|docker)
          groups+=("$2")
          ;;
        *)
          echo "unknown group: $2" >&2
          usage
          exit 2
          ;;
      esac
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ ${#groups[@]} -eq 0 ]]; then
  groups=(generic)
fi

section() {
  local title="$1"
  echo
  echo "== ${title} =="
}

run_log() {
  echo "+ $*"
  "$@" 2>&1 || true
}

log_path_usage() {
  local path="$1"
  if [[ -e "${path}" ]]; then
    run_log du -sh "${path}"
  else
    echo "missing: ${path}"
  fi
}

log_generic() {
  section "Generic runner/root disk"
  run_log date -u
  run_log uname -s
  run_log df -h /
  if [[ "$(uname -s)" == "Linux" ]]; then
    run_log df -i /
  fi
}

log_bazel() {
  section "Bazel disk usage"
  log_path_usage "${HOME}/.cache/bazel"
  log_path_usage "${HOME}/.cache/bazel-repository-cache"
  log_path_usage "${HOME}/.cache/bazel-go-repository-modcache"
  log_path_usage "${HOME}/.cache/bazelisk"
}

log_docker() {
  section "Docker/image-build disk usage"
  run_log docker system df
  run_log docker info
  log_path_usage /var/lib/docker
}

for group in "${groups[@]}"; do
  case "${group}" in
    generic)
      log_generic
      ;;
    bazel)
      log_bazel
      ;;
    docker)
      log_docker
      ;;
  esac
done
