// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

// Step 1: Compile TrieStressTest contract to generate ABI and bin files
//go:generate solc -o . --overwrite --abi --bin --pretty-json --evm-version cancun TrieStressTest.sol
