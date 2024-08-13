// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Calculator = (*dynamicCalculator)(nil)

	ErrCalculatingComplexity = errors.New("error calculating complexity")
	ErrCalculatingGas        = errors.New("error calculating gas")
	ErrCalculatingFee        = errors.New("error calculating fee")
)

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
		return 0, fmt.Errorf("%w: %w", ErrCalculatingComplexity, err)
	}
	gas, err := complexity.ToGas(c.weights)
	if err != nil {
		return 0, fmt.Errorf(
			"%w with complexity (%v) and weights (%v): %w",
			ErrCalculatingGas,
			complexity,
			c.weights,
			err,
		)
	}
	fee, err := gas.Cost(c.price)
	if err != nil {
		return 0, fmt.Errorf(
			"%w with gas (%d) and price (%d): %w",
			ErrCalculatingFee,
			gas,
			c.price,
			err,
		)
	}
	return fee, nil
}
