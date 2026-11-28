#!/usr/bin/env bash
set -euo pipefail

# Install the Linux release pinned by .bazelversion in the current directory.
# TARGETARCH uses Docker architecture names; the argument is the output path.
if [[ "$#" -ne 1 ]]; then
  echo "Usage: $0 <destination>" >&2
  exit 1
fi
destination="$1"

case "${TARGETARCH:?}" in
  amd64) bazel_arch=x86_64 ;;
  arm64) bazel_arch=arm64 ;;
  *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;;
esac

bazel_version="$(<.bazelversion)"
binary="bazel-${bazel_version}-linux-${bazel_arch}"
url="https://releases.bazel.build/${bazel_version}/release/${binary}"

download_dir="$(mktemp -d)"
trap 'rm -rf "${download_dir}"' EXIT

curl -fsSL "${url}" -o "${download_dir}/${binary}"
curl -fsSL "${url}.sha256" -o "${download_dir}/${binary}.sha256"
(cd "${download_dir}" && sha256sum --check "${binary}.sha256")
install -m 0755 "${download_dir}/${binary}" "${destination}"
