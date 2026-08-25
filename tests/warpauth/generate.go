// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import _ "embed"

//go:generate solc --overwrite --abi --bin --evm-version cancun --optimize --optimize-runs 1 --via-ir -o . PChain.sol
//go:generate solc --overwrite --bin-runtime --evm-version cancun --optimize -o . MockWarp.sol
//go:generate rm IWarpMessenger.abi IWarpMessenger.bin

var (
	//go:embed PChain.abi
	PChainABI string
	//go:embed PChain.bin
	PChainBin string
)
