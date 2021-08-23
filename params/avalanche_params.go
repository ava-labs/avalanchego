// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"github.com/ava-labs/avalanchego/utils/units"
)

// Minimum Gas Price
var (
	// MinGasPrice is the number of nAVAX required per gas unit for a
	// transaction to be valid, measured in wei
	LaunchMinGasPrice        int64 = 470_000_000_000
	ApricotPhase1MinGasPrice int64 = 225_000_000_000

	AvalancheAtomicTxFee = units.MilliAvax

	ApricotPhase1GasLimit uint64 = 8_000_000

	ApricotPhase3ExtraDataSize        = 80
	ApricotPhase3MinBaseFee     int64 = 75_000_000_000
	ApricotPhase3MaxBaseFee     int64 = 225_000_000_000
	ApricotPhase3InitialBaseFee int64 = 225_000_000_000
)
