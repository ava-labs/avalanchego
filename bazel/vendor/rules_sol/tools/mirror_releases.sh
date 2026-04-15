#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
VERSIONS="$SCRIPT_DIR/../sol/private/versions.bzl"

# sedd uses gnu-sed on darwin (https://formulae.brew.sh/formula/gnu-sed)
# install on macos with `brew install gnu-sed`
sedd () {
  case $(uname) in
    Darwin*) gsed "$@" ;;
    *) sed "$@" ;;
  esac
}

echo '"Mirrored release data from binaries.soliditylang.org, generated from tools/mirror_releases.sh"' > $VERSIONS
echo "LATEST_VERSION=$(curl --silent --location "https://binaries.soliditylang.org/bin/list.json" | jq .latestRelease)" >> $VERSIONS
echo 'TOOL_VERSIONS={}' >> $VERSIONS

for plat in macosx-amd64 linux-amd64 windows-amd64; do
  RAW=$(mktemp)
  url="https://binaries.soliditylang.org/$plat/list.json"
  printf "# Vendored from %s\nTOOL_VERSIONS[\"%s\"] = %s\n" "$url" "${plat}" "$(
    curl --silent --location "$url" | jq -f "$SCRIPT_DIR/filter.jq"
  )" > $RAW

  sedd -r 's#(.*)"([0-9a-f]{64})"#printf "    \\"sha256\\": \\"sha256-%s\\"" $(echo \2 | xxd -p -r | base64)#e' < $RAW >> $VERSIONS
  rm "$RAW"
done
