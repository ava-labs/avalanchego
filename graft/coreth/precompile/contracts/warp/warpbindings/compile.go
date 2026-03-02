// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpbindings

// Step 1: Compile IWarpMessenger interface to generate ABI at parent level
//go:generate solc -o .. --overwrite --abi --pretty-json --evm-version cancun IWarpMessenger.sol
