#!/usr/bin/env bash

# Inspect the real image graph in the native builder. No command substitutes or
# remote-cache credentials are needed. Run image input preparation first.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-${repo_root}/.cache/bazel-image}"
case "$(uname -m)" in
  x86_64) architecture=amd64 ;;
  aarch64 | arm64) architecture=arm64 ;;
  *) echo 'unsupported builder architecture' >&2; exit 1 ;;
esac
root="$(mktemp -d "${cache_root}/test-builder-cache.XXXXXX")"
trap 'echo "Builder cache test outputs: $root"' EXIT
builder_tag=avalanchego-bazel-builder:local
builder_digest="$(docker image inspect --format '{{.Id}}' "$builder_tag")"

# Docker operations belong on the host. The builder must not carry a Docker
# installation or require access to the host daemon.
docker run --pull=never --rm --network none "$builder_tag" /bin/sh -ec '
  if command -v docker || command -v dockerd || command -v containerd || command -v runc; then
    echo "builder contains an unnecessary Docker installation" >&2
    exit 1
  fi
  test ! -S /var/run/docker.sock
'

for name in first repeat changed; do
  digest="$builder_digest"
  if [[ "$name" == changed ]]; then
    digest="sha256:$(printf '%064d' 0)"
  fi
  docker run --pull=never --rm \
    --user "$(id -u):$(id -g)" \
    --workdir /workspace \
    --env HOME=/cache/home \
    --mount "type=bind,src=$repo_root,dst=/workspace,readonly" \
    --mount "type=bind,src=$cache_root/inner-repository,dst=/cache/repository" \
    --mount "type=bind,src=$cache_root/inner-go-modules,dst=/cache/go-modules" \
    --mount "type=bind,src=$root,dst=/cache" \
    "$builder_tag" /usr/local/bin/bazel \
    --batch --output_user_root=/cache/output \
    aquery 'mnemonic("GoCompilePkg|GoStdlib|GoLink|FileWrite", deps(//bazel/image:push_avalanchego))' \
    --output=jsonproto --include_file_write_contents \
    --repository_cache=/cache/repository \
    --repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1 \
    --repo_env=GOMODCACHE=/cache/go-modules \
    --repository_disable_download --repo_env=GOPROXY=off \
    --lockfile_mode=error --disk_cache=/cache/disk \
    --config=release --compilation_mode=opt \
    --extra_execution_platforms="//bazel/image:builder_linux_${architecture}" \
    --platforms="//bazel/image:linux_${architecture}" \
    --//bazel/toolchains:builder_digest="$digest" \
    > "$root/$name.json"

  # Follow aquery's artifact/depset references instead of depending on their
  # numeric IDs, which can change between invocations.
  jq -e --arg digest "$digest" '
    (.pathFragments | map({key: (.id | tostring), value: .}) | from_entries) as $paths |
    def path($id): $paths[$id | tostring] | if .parentId then path(.parentId) + "/" + .label else .label end;
    (.artifacts | map({key: (.id | tostring), value: path(.pathFragmentId)}) | from_entries) as $files |
    (.depSetOfFiles | map({key: (.id | tostring), value: .}) | from_entries) as $sets |
    def inputs($id): $sets[$id | tostring] | (.directArtifactIds[]? | $files[. | tostring]), (.transitiveDepSetIds[]? | inputs(.));
    [.actions[] | select(.mnemonic == "FileWrite") |
      select(any(.outputIds[]; $files[. | tostring] | endswith("/builder_identity.txt"))) |
      select(.fileContents == ($digest + "\n"))] as $markers |
    [.actions[] | select(.mnemonic == "GoStdlib" or .mnemonic == "GoLink" or
      (.mnemonic == "GoCompilePkg" and any(.environmentVariables[]?; .key == "CC")))] as $actions |
    if ($markers | length) == 0 then error("builder digest is not a declared file input")
    elif ([$actions[] | select(.mnemonic == "GoLink")] | length) != 2 then error("expected both platform binaries")
    elif ([$actions[] | select(.mnemonic == "GoStdlib")] | length) != 2 then error("expected both platform standard libraries")
    elif all($actions[]; any(.inputDepSetIds[] | inputs(.); endswith("/builder_identity.txt"))) then
      {checked_actions: ($actions | length),
       identity_actions: ($markers | map(.actionKey) | sort),
       compiler_actions: ($actions | map(.actionKey) | sort)}
    else error("a native or cross-compilation action lacks the builder identity input") end
  ' "$root/$name.json" > "$root/$name.summary.json"
done

cmp "$root/first.summary.json" "$root/repeat.summary.json"
if cmp -s "$root/first.summary.json" "$root/changed.summary.json"; then
  echo 'changing the builder digest did not change the action graph' >&2
  exit 1
fi
echo 'Builder digest input checks passed for identical and changed digests'
