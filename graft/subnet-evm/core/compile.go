// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

// Step 1: Compile TrieStressTest contract to generate ABI and bin files
//go:generate solc-v0.8.30 -o . --overwrite --abi --bin --pretty-json --evm-version cancun TrieStressTest.sol
