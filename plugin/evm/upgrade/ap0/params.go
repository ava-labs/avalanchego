// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP0 defines constants used during the initial network launch.
package ap0

import (
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/utils"
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
)
