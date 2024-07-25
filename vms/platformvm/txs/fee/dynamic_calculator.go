// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Calculator = (*dynamicCalculator)(nil)

func NewDynamicCalculator(
	weights fee.Dimensions,
	price fee.GasPrice,
) Calculator {
	return &dynamicCalculator{
		weights: weights,
		price:   price,
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
	return gas.Cost(c.price)
}
