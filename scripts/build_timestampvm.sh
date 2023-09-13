#!/usr/bin/env bash
# (c) 2019-2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

# e.g.,
# ./scripts/build_timestampvm.sh
if ! [[ "$0" =~ scripts/build_timestampvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

#################################
# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

# Load the constants
# Set the PATHS
GOPATH="$(go env GOPATH)"

if [[ $# -eq 1 ]]; then
    binary_directory=$1
elif [[ $# -eq 0 ]]; then
    binary_directory="$GOPATH/src/github.com/ava-labs/avalanchego/build/plugins/tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"
else
    echo "Invalid arguments to build timestampvm. Requires either no arguments (default) or one arguments to specify binary location."
    exit 1
fi

# Build timestampvm, which is run as a subprocess
echo "Building timestampvm with output as $binary_directory"
go build -o "$binary_directory" "vms/example/timestampvm/main/"*.go
