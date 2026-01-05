// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpbindings

// Step 1: Compile IWarpMessenger interface to generate ABI at parent level
//go:generate solc-v0.8.30 -o .. --overwrite --abi --pretty-json --evm-version cancun IWarpMessenger.sol
