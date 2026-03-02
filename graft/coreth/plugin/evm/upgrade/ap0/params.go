// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP0 defines constants used during the initial network launch.
package ap0

import (
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// MinGasPrice is the minimum gas price of a transaction.
	//
	// This value was modified by `ap1.MinGasPrice`.
	MinGasPrice = 470 * utils.GWei

	// AtomicTxFee is the amount of AVAX that must be burned by an atomic tx.
	//
	// This value was replaced with the Apricot Phase 3 dynamic fee mechanism.
	AtomicTxFee = units.MilliAvax

	// Note: MaximumExtraDataSize has been reduced to 32 in Geth, but is kept the same in Coreth for
	// backwards compatibility.
	MaximumExtraDataSize = 64 // Maximum size extra data may be after Genesis.

	MinGasLimit          = 5000               // Minimum the gas limit may ever be.
	MaxGasLimit          = 0x7fffffffffffffff // Maximum the gas limit (2^63-1).
	GasLimitBoundDivisor = 1024               // The bound divisor of the gas limit, used in update calculations.
)
