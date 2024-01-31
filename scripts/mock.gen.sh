#!/usr/bin/env bash

set -euo pipefail

# Root directory
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# https://github.com/uber-go/mock
go install -v go.uber.org/mock/mockgen@v0.4.0

if ! command -v go-license &>/dev/null; then
  echo "go-license not found, installing..."
  # https://github.com/palantir/go-license
  go install -v github.com/palantir/go-license@v1.25.0
fi

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# tuples of (source interface import path, comma-separated interface names, output file path)
input="scripts/mocks.mockgen.txt"
while IFS= read -r line; do
  IFS='=' read -r src_import_path interface_name output_path <<<"${line}"
  package_name=$(basename "$(dirname "$output_path")")
  echo "Generating ${output_path}..."
  mockgen -package="${package_name}" -destination="${output_path}" "${src_import_path}" "${interface_name}"

  go-license \
    --config=./header.yml \
    "${output_path}"
done <"$input"

echo "SUCCESS"
