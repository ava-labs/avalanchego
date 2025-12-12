#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -x

script_dir=$(dirname "$0")

sed_command="1{/The go-ethereum Authors/{r ${script_dir}/avalanche_header.txt
        N
    }
}"
sed -i '' -e "${sed_command}" "$@"