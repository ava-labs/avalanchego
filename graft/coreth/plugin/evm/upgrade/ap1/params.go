// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP1 defines constants used after the Apricot Phase 1 upgrade.
package ap1

import "github.com/ava-labs/avalanchego/graft/coreth/utils"

const (
	// MinGasPrice is the minimum gas price of a transaction after the Apricot
	// Phase 1 upgrade.
	//
	// This value was replaced with the Apricot Phase 3 dynamic fee mechanism.
	MinGasPrice = 225 * utils.GWei

	// GasLimit is the target amount of gas that can be included in a single
	// block after the Apricot Phase 1 upgrade.
	//
	// This value encodes the default parameterization of the initial gas
	// targeting mechanism.
	GasLimit = 8_000_000
)
