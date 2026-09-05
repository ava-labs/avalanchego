// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchainhelper embeds the compiled CChainHelper contract, the trusted
// C-chain helper that exports to and imports from the P-chain on behalf of
// msg.sender (see vms/saevm/cchain/hooks.go and tx/warp_credential.go).
package cchainhelper

import _ "embed"

//go:generate solc --overwrite --abi --bin --evm-version cancun --optimize -o . CChainHelper.sol
//go:generate rm IWarpMessenger.abi IWarpMessenger.bin

var (
	//go:embed CChainHelper.abi
	ABI string
	//go:embed CChainHelper.bin
	Bin string
)
