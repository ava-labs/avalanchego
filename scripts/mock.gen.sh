#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# https://github.com/uber-go/mock
go install -v go.uber.org/mock/mockgen@v0.4.0

source ./scripts/constants.sh

outputted_files=()

# tuples of (source import path, comma-separated interface names, output file path)
input="scripts/mocks.mockgen.txt"
while IFS= read -r line
do
  IFS='=' read -r src_import_path interface_name output_path <<< "${line}"
  package_name="$(basename "$(dirname "$output_path")")"
  echo "Generating ${output_path}..."
  outputted_files+=("${output_path}")
  mockgen -package="${package_name}" -destination="${output_path}" -mock_names="${interface_name}=${interface_name}" "${src_import_path}" "${interface_name}"

done < "$input"

# tuples of (source import path, comma-separated interface names to exclude, interface name, output file path)
input="scripts/mocks.mockgen.source.txt"
while IFS= read -r line
do
  IFS='=' read -r source_path exclude_interfaces interface_name output_path <<< "${line}"
  package_name=$(basename "$(dirname "$output_path")")
  outputted_files+=("${output_path}")
  echo "Generating ${output_path}..."

  mockgen \
    -source="${source_path}" \
    -destination="${output_path}" \
    -package="${package_name}" \
    -exclude_interfaces="${exclude_interfaces}" \
    -mock_names="${interface_name}=${interface_name}"

done < "$input"

mapfile -t all_generated_files < <(grep -Rl 'Code generated by MockGen. DO NOT EDIT.')

# Exclude certain files
outputted_files+=('scripts/mock.gen.sh') # This file
outputted_files+=('vms/components/avax/avaxmock/transferable_out.go') # Embedded verify.IsState
outputted_files+=('vms/platformvm/fx/fxmock/owner.go') # Embedded verify.IsNotState
outputted_files+=('vms/proposervm/mock_post_fork_block.go') # Causes an import cycle if put in sub-dir proposervmmock
outputted_files+=('x/sync/mock_client.go') # Causes an import cycle if put in sub-dir syncmock
outputted_files+=('vms/platformvm/state/mock_staker_iterator.go') # Causes an import cycle if put in sub-dir statemock
outputted_files+=('vms/avm/block/mock_block.go') # idk tbh
outputted_files+=('vms/platformvm/state/mock_state.go') # idk tbh
outputted_files+=('vms/platformvm/block/mock_block.go') # idk tbh

mapfile -t diff_files < <(echo "${all_generated_files[@]}" "${outputted_files[@]}" | tr ' ' '\n' | sort | uniq -u)

if (( ${#diff_files[@]} )); then
  printf "\nFAILURE\n"
  echo "Detected MockGen generated files that are not in scripts/mocks.mockgen.source.txt or scripts/mocks.mockgen.txt:"
  printf "%s\n" "${diff_files[@]}"
  exit 255
fi

echo "SUCCESS"
