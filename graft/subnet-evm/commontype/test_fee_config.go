// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import "math/big"

var ValidTestFeeConfig = FeeConfig{
	GasLimit:        big.NewInt(8_000_000),
	TargetBlockRate: 2, // in seconds

	MinBaseFee:               big.NewInt(25_000_000_000),
	TargetGas:                big.NewInt(15_000_000),
	BaseFeeChangeDenominator: big.NewInt(36),

	MinBlockGasCost:  big.NewInt(0),
	MaxBlockGasCost:  big.NewInt(1_000_000),
	BlockGasCostStep: big.NewInt(200_000),
}

var ValidTestACP224FeeConfig = ACP224FeeConfig{
	TargetGas:         big.NewInt(15_000_000),
	MinGasPrice:       big.NewInt(1),
	MaxCapacityFactor: big.NewInt(100),
	TimeToDouble:      big.NewInt(60),
}
