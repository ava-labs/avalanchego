#!/bin/bash

set -e

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

if ! command -v mockgen &> /dev/null
then
  echo "mockgen not found, installing..."
  # https://github.com/golang/mock
  go install -v github.com/golang/mock/mockgen@v1.6.0
fi

if ! command -v go-license &> /dev/null
then
  echo "go-license not found, installing..."
  # https://github.com/palantir/go-license
  go install -v github.com/palantir/go-license@v1.25.0
fi

# tuples of (source interface import path, comma-separated interface names, output file path)
input="scripts/mocks.mockgen.txt"
while IFS= read -r line
do
  IFS='=' read src_import_path interface_name output_path <<< "${line}"
  package_name=$(basename $(dirname $output_path))
  echo "Generating ${output_path}..."
  mockgen -package=${package_name} -destination=${output_path} ${src_import_path} ${interface_name}
  
  go-license \
  --config=./header.yml \
  "${output_path}"
done < "$input"

echo "SUCCESS"
