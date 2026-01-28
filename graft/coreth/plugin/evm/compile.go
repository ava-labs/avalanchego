// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

// Using shanghai EVM version is necessary in this case, as more recent solc optimizations change the binary output.
// See https://github.com/ava-labs/avalanchego/actions/runs/21188583091/job/60949476635?pr=4888 as an example.

// Step 1: Compile ExampleWarp contract to generate ABI and bin files
//go:generate sh -c "solc -o . --overwrite --abi --bin --pretty-json --base-path ../.. --evm-version shanghai ExampleWarp.sol && rm -f IWarpMessenger.abi IWarpMessenger.bin"
