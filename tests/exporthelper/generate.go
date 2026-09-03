// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package exporthelper embeds the compiled ExportHelper contract, the trusted
// C-chain export helper (see vms/saevm/cchain/hooks.go).
package exporthelper

import _ "embed"

//go:generate solc --overwrite --abi --bin --evm-version cancun --optimize -o . ExportHelper.sol
//go:generate rm IWarpMessenger.abi IWarpMessenger.bin

var (
	//go:embed ExportHelper.abi
	ABI string
	//go:embed ExportHelper.bin
	Bin string
)
