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
    # Stream the archive into ctr. Writing an archive with docker cp is not
    # reliable for Kind nodes because the target mount namespace can differ
    # from the one used by the container runtime.
    docker exec -i "$node" ctr -n k8s.io images import - <"$archive"
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

  # Kind creates the node image from a digest reference, which leaves the
  # Docker image untagged. Give it the version tag recorded by the running
  # node so an offline consumer can select it without a registry lookup.
  local kind_node_image_id kind_node_image_ref
  kind_node_image_id="$(docker inspect --format '{{.Image}}' kind-control-plane)"
  kind_node_image_ref="$(docker inspect --format '{{.Config.Image}}' kind-control-plane)"
  kind_node_image_ref="${kind_node_image_ref%%@*}"
  case "$kind_node_image_ref" in
    kindest/node:*) docker image tag "$kind_node_image_id" "$kind_node_image_ref" ;;
    *)
      echo "unexpected Kind node image reference: $kind_node_image_ref" >&2
      exit 1
      ;;
  esac

  image_ids >"$snapshot_dir/host-after.txt"
  comm -13 "$snapshot_dir/host-before.txt" "$snapshot_dir/host-after.txt" >"$snapshot_dir/host-delta.txt"
  if [[ -s "$snapshot_dir/host-delta.txt" ]]; then
    local -a host_image_ids host_image_refs
    mapfile -t host_image_ids <"$snapshot_dir/host-delta.txt"
    # Saving an image ID discards its repository tags. The offline workload
    # starts registry:2 by tag, so preserve every tag when one is available.
    # Keep an ID only for a dangling local image.
    for image_id in "${host_image_ids[@]}"; do
      local -a image_tags valid_image_tags
      mapfile -t image_tags < <(docker image inspect --format '{{range .RepoTags}}{{println .}}{{end}}' "$image_id")
      for image_tag in "${image_tags[@]}"; do
        # Docker prints template sentinel values for images without tags.
        # Do not pass those as image references to `docker image save`.
        case "$image_tag" in
          ''|'<no value>'|'<nil>') continue ;;
        esac
        valid_image_tags+=("$image_tag")
      done
      if (( ${#valid_image_tags[@]} )); then
        host_image_refs+=("${valid_image_tags[@]}")
      else
        host_image_refs+=("$image_id")
      fi
    done
    docker image save --output "$output_dir/host-images.tar" "${host_image_refs[@]}"
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
    # Kubernetes can discard content blobs after it unpacks an image, leaving
    # the k8s.io image metadata unexportable. Re-pull only the discovered
    # canonical references into a temporary namespace before exporting them.
    # The producer is online; the consumer is still validated offline.
    local -a containerd_refs
    mapfile -t containerd_refs < <(grep '/' "$snapshot_dir/containerd-delta.txt")
    for image_ref in "${containerd_refs[@]}"; do
      docker exec kind-control-plane ctr -n kind-image-cache images pull --platform linux/amd64 "$image_ref"
    done
    # Stream the local export through docker exec instead of writing to the
    # node filesystem: containerd's transfer service may use a distinct mount
    # namespace, which makes a named output path unavailable to docker cp.
    docker exec kind-control-plane ctr -n kind-image-cache images export --local --platform linux/amd64 /dev/stdout "${containerd_refs[@]}" >"$output_dir/containerd-images.tar"
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
