#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# https://github.com/golang/mock
go install -v github.com/golang/mock/mockgen@v1.6.0

source ./scripts/constants.sh

# tuples of (source interface import path, comma-separated interface names, output file path)
input="scripts/mocks.mockgen.txt"
while IFS= read -r line
do
  IFS='=' read -r src_import_path interface_name output_path <<< "${line}"
  package_name=$(basename "$(dirname "$output_path")")
  [[ $src_import_path == \#* ]] && continue
  echo "Generating ${output_path}..."
  mockgen -copyright_file=./LICENSE.header -package="${package_name}" -destination="${output_path}" "${src_import_path}" "${interface_name}"
done < "$input"

echo "SUCCESS"
