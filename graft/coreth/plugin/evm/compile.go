// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

// Step 1: Compile ExampleWarp contract to generate ABI and bin files
//go:generate sh -c "solc-v0.8.30 -o . --overwrite --abi --bin --pretty-json --base-path ../.. --evm-version shanghai ExampleWarp.sol && rm -f IWarpMessenger.abi IWarpMessenger.bin"
