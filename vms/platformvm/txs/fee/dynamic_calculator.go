// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Calculator = (*dynamicCalculator)(nil)

func NewDynamicCalculator(
	config fee.Config,
	excess fee.Gas,
) Calculator {
	return &dynamicCalculator{
		weights: config.Weights,
		price:   config.MinGasPrice.MulExp(excess, config.ExcessConversionConstant),
	}
}

type dynamicCalculator struct {
	weights fee.Dimensions
	price   fee.GasPrice
}

func (c *dynamicCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	complexity, err := TxComplexity(tx)
	if err != nil {
		return 0, err
	}
	gas, err := complexity.ToGas(c.weights)
	if err != nil {
		return 0, err
	}
	return math.Mul(uint64(c.price), uint64(gas))
}
