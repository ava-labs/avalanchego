#!/usr/bin/env bash
set -euo pipefail

case "$(uname -m)" in
  x86_64) platform=linux/amd64 ;;
  aarch64|arm64) platform=linux/arm64 ;;
  *) echo "unsupported host architecture: $(uname -m)" >&2; exit 1 ;;
esac

image="${BAZEL_IMAGE:-avalanchego:bazel-local}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
git_commit="${AVALANCHEGO_COMMIT:-$(git -C "${repo_root}" rev-parse HEAD)}"
cd "${repo_root}"
mkdir -p build
config_dir="$(mktemp -d "${repo_root}/build/bazel-image-config-XXXXXX")"
trap 'rm -rf "${config_dir}"' EXIT

# CI consumes the caches that setup-bazel prepared on the host. Elsewhere the
# Dockerfile's prepare stage fetches dependencies inside the Linux container
# with network access, so any host can build the image. Bazel repository rules
# fetch toolchains for the platform they run on, so host preparation would only
# work on Linux.
if [[ "${GITHUB_ACTIONS:-}" == true ]]; then
  repository_cache="${BAZEL_REPOSITORY_CACHE:-${HOME}/.cache/bazel-repository-cache}"
  go_mod_cache="${BAZEL_GO_MOD_CACHE:-${HOME}/.cache/bazel-go-repository-modcache}"
  [[ -d "${repository_cache}" && -d "${go_mod_cache}" && -f .bazelrc.local ]] || {
    echo 'Bazel configuration and prepared dependency caches are required' >&2
    exit 1
  }
  repository_cache="$(cd "${repository_cache}" && pwd)"
  go_mod_cache="$(cd "${go_mod_cache}" && pwd)"
  cache_args=(
    --build-context "bazel_repository_cache=${repository_cache}"
    --build-context "bazel_go_mod_cache=${go_mod_cache}"
  )
else
  # These are container paths, backed by BuildKit cache mounts.
  repository_cache=/var/cache/bazel-repository
  go_mod_cache=/var/cache/bazel-go-modules
  cache_args=()
fi
# Preserve workspace settings without changing the checkout's configuration.
if [[ -f .bazelrc.local ]]; then
  cp .bazelrc.local "${config_dir}/.bazelrc.local"
fi
if [[ "${GITHUB_ACTIONS:-}" != true ]]; then
  # Local preparation uses container cache paths. CI already supplies its paths
  # and authentication through .bazelrc.local; inherit those settings unchanged.
  cat >> "${config_dir}/.bazelrc.local" <<EOF

# Bazel image dependency caches
common --repository_cache=${repository_cache}
common --repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1
common --repo_env=GOMODCACHE=${go_mod_cache}
EOF
fi

# Remote-cache access requires network access during compilation. setup-bazel
# sets this flag in CI; local users set it when configuring a remote cache.
build_mode=offline
if [[ "${BAZEL_REMOTE_CACHE_CONFIGURED:-false}" == true ]]; then
  build_mode=online
fi

docker buildx build --load --platform "${platform}" \
  --build-arg AVALANCHEGO_COMMIT="${git_commit}" \
  --build-arg "BAZEL_BUILD_MODE=${build_mode}" \
  --build-arg "BAZEL_REPOSITORY_CACHE=${repository_cache}" \
  --build-arg "BAZEL_GO_MOD_CACHE=${go_mod_cache}" \
  --build-arg "BAZEL_CACHE_NAMESPACE=${BAZEL_CACHE_NAMESPACE:-avalanchego-bazel}" \
  --build-arg BAZEL_INVOCATION_KEY="$(date +%s)-$$" \
  ${cache_args[@]+"${cache_args[@]}"} \
  --secret "id=bazelrc,src=${config_dir}/.bazelrc.local" \
  -t "${image}" \
  -f "${repo_root}/Dockerfile.bazel" "${repo_root}"
echo "Built ${image} for ${platform}"
version_output="$(docker run --rm "${image}" ./avalanchego --version)"
printf '%s\n' "${version_output}"
if [[ "${version_output}" != *"commit=${git_commit},"* ]]; then
  echo "Image commit does not match the expected commit (${git_commit})" >&2
  exit 1
fi
echo 'Image version smoke test passed'
