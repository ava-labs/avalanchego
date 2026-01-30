// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Module to facilitate the registration of precompiles and their configuration.
package registry

// Force imports of each precompile to ensure each precompile's init function runs and registers itself
// with the registry.
import (
	_ "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	// ADD PRECOMPILES BELOW
	// _ "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/newprecompile"
)

// This list is kept just for reference. The actual addresses defined in respective packages of precompiles.
// Note: it is important that none of these addresses conflict with each other or any other precompiles
// in /coreth/contracts/contracts/**.

// WarpMessengerAddress = common.HexToAddress("0x0200000000000000000000000000000000000005")
// ADD PRECOMPILES BELOW
// NewPrecompileAddress = common.HexToAddress("0x02000000000000000000000000000000000000??")
