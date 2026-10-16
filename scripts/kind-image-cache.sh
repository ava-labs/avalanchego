#!/usr/bin/env bash
# Manage the image cache used by Kind workloads. The cache deliberately records
# deltas; it must never export the images preinstalled on a CI runner.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
profile="kind-images"

usage() {
  cat >&2 <<'EOF'
Usage: scripts/kind-image-cache.sh <command> [arguments]

Commands:
  input-digest                         Print the cache-input digest.
  snapshot-containerd <directory>      Record Kind node images before bootstrap.
  produce <directory>                  Run robustness and write image-cache delta.
  import-containerd <archive>          Import an archive into every Kind node.
EOF
  exit 2
}

input_digest() {
  # Keep this list aligned with the Bazel target's data. Image references can
  # also be introduced through the embedded manifests below.
  local inputs=(
    flake.lock
    flake.nix
    Taskfile.yml
    scripts/build_tmpnetctl.sh
    scripts/kind-image-cache.sh
    scripts/kind-with-registry.sh
    scripts/produce-kind-image-cache.sh
    scripts/run_task.sh
    scripts/start_kind_cluster.sh
    tests/fixture/tmpnet/kind_image_cache_completeness_test.sh
    .github/actions/setup-kind-image-cache/action.yml
    .github/actions/restore-kind-image-cache/action.yml
    tests/fixture/tmpnet/start_kind_cluster.go
    tests/fixture/tmpnet/tmpnetctl/main.go
  )
  while IFS= read -r -d '' file; do
    inputs+=("${file#"$repo_root"/}")
  done < <(find "$repo_root/tests/fixture/tmpnet/yaml" -type f -print0 | sort -z)

  (
    cd "$repo_root"
    sha256sum "${inputs[@]}"
  ) | sha256sum | awk '{print $1}'
}

snapshot_containerd() {
  local output_dir="$1"
  mkdir -p "$output_dir"
  docker exec kind-control-plane ctr -n k8s.io images ls -q | LC_ALL=C sort -u >"$output_dir/containerd-before.txt"
}

import_containerd() {
  local archive="$1"
  [[ -r "$archive" ]] || {
    echo "Kind containerd image archive is not readable: $archive" >&2
    exit 1
  }

  local node
  while IFS= read -r node; do
    docker cp "$archive" "$node:/tmp/containerd-images.tar"
    docker exec "$node" ctr -n k8s.io images import /tmp/containerd-images.tar
    docker exec "$node" rm /tmp/containerd-images.tar
  done < <(kind get nodes)
}

image_ids() {
  docker image ls --no-trunc --format '{{.ID}}' | LC_ALL=C sort -u
}

produce() {
  local output_dir="$1"
  local snapshot_dir
  snapshot_dir="$(mktemp -d)"
  trap 'rm -rf "${snapshot_dir:-}"' EXIT

  rm -rf "$output_dir"
  mkdir -p "$output_dir"
  image_ids >"$snapshot_dir/host-before.txt"

  # kind-with-registry.sh takes this snapshot after kind create and before any
  # chart or tmpnet component can pull an image.
  mkdir -p "$snapshot_dir/helm/config" "$snapshot_dir/helm/cache"
  HELM_CONFIG_HOME="$snapshot_dir/helm/config" \
  HELM_CACHE_HOME="$snapshot_dir/helm/cache" \
  AVALANCHEGO_KIND_IMAGE_CACHE_SNAPSHOT_DIR="$snapshot_dir" \
    "$repo_root/scripts/run_task.sh" test-robustness
  tar --create --file "$output_dir/helm-cache.tar" -C "$snapshot_dir/helm" config cache

  image_ids >"$snapshot_dir/host-after.txt"
  comm -13 "$snapshot_dir/host-before.txt" "$snapshot_dir/host-after.txt" >"$snapshot_dir/host-delta.txt"
  if [[ -s "$snapshot_dir/host-delta.txt" ]]; then
    local -a host_image_ids
    mapfile -t host_image_ids <"$snapshot_dir/host-delta.txt"
    docker image save --output "$output_dir/host-images.tar" "${host_image_ids[@]}"
  else
    tar --create --file "$output_dir/host-images.tar" --files-from /dev/null
  fi

  [[ -f "$snapshot_dir/containerd-before.txt" ]] || {
    echo "Kind did not create the containerd inventory snapshot" >&2
    exit 1
  }
  docker exec kind-control-plane ctr -n k8s.io images ls -q | LC_ALL=C sort -u >"$snapshot_dir/containerd-after.txt"
  comm -13 "$snapshot_dir/containerd-before.txt" "$snapshot_dir/containerd-after.txt" >"$snapshot_dir/containerd-delta.txt"
  if [[ -s "$snapshot_dir/containerd-delta.txt" ]]; then
    docker cp "$snapshot_dir/containerd-delta.txt" kind-control-plane:/tmp/containerd-delta.txt
    docker exec kind-control-plane sh -c 'ctr -n k8s.io images export /tmp/containerd-images.tar $(cat /tmp/containerd-delta.txt)'
    docker cp kind-control-plane:/tmp/containerd-images.tar "$output_dir/containerd-images.tar"
    docker exec kind-control-plane rm -f /tmp/containerd-images.tar /tmp/containerd-delta.txt
  else
    tar --create --file "$output_dir/containerd-images.tar" --files-from /dev/null
  fi

  {
    echo "profile=$profile"
    echo "input_digest=$(input_digest)"
    echo "host_images_sha256=$(sha256sum "$output_dir/host-images.tar" | awk '{print $1}')"
    echo "containerd_images_sha256=$(sha256sum "$output_dir/containerd-images.tar" | awk '{print $1}')"
    echo "helm_cache_sha256=$(sha256sum "$output_dir/helm-cache.tar" | awk '{print $1}')"
    echo "[host image delta]"
    while IFS= read -r image_id; do
      docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}{{range .RepoTags}}{{println .}}{{end}}' "$image_id"
    done <"$snapshot_dir/host-delta.txt" | LC_ALL=C sort -u
    echo "[containerd image delta]"
    while IFS= read -r image_ref; do
      docker exec kind-control-plane ctr -n k8s.io images ls | awk -v image_ref="$image_ref" '$1 == image_ref { print $1 " " $3 }'
    done <"$snapshot_dir/containerd-delta.txt" | LC_ALL=C sort -u
  } >"$output_dir/manifest"
}

case "${1:-}" in
  input-digest) input_digest ;;
  snapshot-containerd) [[ $# == 2 ]] || usage; snapshot_containerd "$2" ;;
  import-containerd) [[ $# == 2 ]] || usage; import_containerd "$2" ;;
  produce) [[ $# == 2 ]] || usage; produce "$2" ;;
  *) usage ;;
esac
