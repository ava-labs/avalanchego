// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import "math/big"

// Minimum Gas Price
var (
	// MinGasPrice is the number of nAVAX required per gas unit for a
	// transaction to be valid, measured in wei
	LaunchMinGasPrice        = big.NewInt(470 * GWei)
	ApricotPhase1MinGasPrice = big.NewInt(225 * GWei)

	ApricotPhase1GasLimit uint64 = 8_000_000

	ApricotPhase3ExtraDataSize        = 80
	ApricotPhase3MinBaseFee     int64 = 25_000_000_000
	ApricotPhase3MaxBaseFee     int64 = 225_000_000_000
	ApricotPhase3InitialBaseFee int64 = 225_000_000_000
)
