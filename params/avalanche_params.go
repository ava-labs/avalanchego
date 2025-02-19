// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"github.com/ava-labs/avalanchego/utils/units"
)

// Minimum Gas Price
const (
	// MinGasPrice is the number of nAVAX required per gas unit for a
	// transaction to be valid, measured in wei
	LaunchMinGasPrice        int64 = 470 * GWei
	ApricotPhase1MinGasPrice int64 = 225 * GWei

	AvalancheAtomicTxFee = units.MilliAvax

	ApricotPhase1GasLimit uint64 = 8_000_000
	CortinaGasLimit       uint64 = 15_000_000
	EtnaMinBaseFee        int64  = GWei
)
