// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import _ "embed"

//go:generate solc --overwrite --abi --bin --evm-version cancun --optimize -o . PChain.sol

var (
	//go:embed PChain.abi
	PChainABI string
	//go:embed PChain.bin
	PChainBin string
)
