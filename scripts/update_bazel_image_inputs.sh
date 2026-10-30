#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_file="$repo_root/MODULE.bazel"
image_script="$repo_root/scripts/build_image_bazel.sh"
image="docker.io/library/ubuntu:24.04"

raw_index="$(docker buildx imagetools inspect --raw "$image")"
index_digest="$(docker buildx imagetools inspect "$image" | awk '/^Digest:/ {print $2}')"
amd64_digest="$(jq -r '.manifests[] | select(.platform.os == "linux" and .platform.architecture == "amd64") | .digest' <<<"$raw_index")"
arm64_digest="$(jq -r '.manifests[] | select(.platform.os == "linux" and .platform.architecture == "arm64") | .digest' <<<"$raw_index")"
created="$(jq -r '.manifests[] | select(.platform.os == "linux" and .platform.architecture == "amd64") | .annotations["org.opencontainers.image.created"]' <<<"$raw_index")"

for value in "$index_digest" "$amd64_digest" "$arm64_digest" "$created"; do
  if [[ -z "$value" || "$value" == null ]]; then
    echo "Ubuntu 24.04 index did not provide all required Linux manifests" >&2
    exit 1
  fi
done

# The image creation date is immutable metadata in the selected index. It makes
# the package snapshot reproducible and avoids advancing a timestamp when the
# selected image has not changed.
snapshot_timestamp="$(date --utc --date="$created" +%Y%m%dT%H%M%SZ)"
current_digest="$(awk '/name = "ubuntu_24_04"/ {found=1} found && /digest = / {split($0, fields, "\""); print fields[2]; exit}' "$module_file")"
if [[ "$current_digest" == "$index_digest" ]]; then
  echo "Ubuntu 24.04 image index is already pinned: $index_digest"
  exit 0
fi

python3 - "$module_file" "$image_script" "$index_digest" "$snapshot_timestamp" "$amd64_digest" "$arm64_digest" <<'PY'
import pathlib
import re
import sys

module_path, script_path, index_digest, snapshot, amd64, arm64 = sys.argv[1:]
module = pathlib.Path(module_path)
script = pathlib.Path(script_path)
module_text = module.read_text()
script_text = script.read_text()
module_text, count = re.subn(
    r'(name = "ubuntu_24_04",\n    digest = ")[^"]+',
    rf'\g<1>{index_digest}',
    module_text,
    count=1,
)
if count != 1:
    raise SystemExit("could not update the Ubuntu image index in MODULE.bazel")
module_text, count = re.subn(
    r'(https://snapshot\.ubuntu\.com/ubuntu/)[^"]+',
    rf'\g<1>{snapshot}',
    module_text,
    count=1,
)
if count != 1:
    raise SystemExit("could not update the Ubuntu snapshot in MODULE.bazel")
replacements = {
    r'(ubuntu_base_image_amd64="docker\.io/library/ubuntu@)[^"]+': rf'\g<1>{amd64}',
    r'(ubuntu_base_image_arm64="docker\.io/library/ubuntu@)[^"]+': rf'\g<1>{arm64}',
    r'(ubuntu_base_image_amd64_tag="avalanchego-ci/ubuntu-24\.04:)[^-]+(?=-amd64")': rf'\g<1>{amd64.removeprefix("sha256:")}',
    r'(ubuntu_base_image_arm64_tag="avalanchego-ci/ubuntu-24\.04:)[^-]+(?=-arm64")': rf'\g<1>{arm64.removeprefix("sha256:")}',
}
for pattern, replacement in replacements.items():
    script_text, count = re.subn(pattern, replacement, script_text, count=1)
    if count != 1:
        raise SystemExit(f"could not update {pattern} in build_image_bazel.sh")
module.write_text(module_text)
script.write_text(script_text)
PY

cd "$repo_root"
task bazel-generate-metadata
