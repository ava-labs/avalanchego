// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Calculator = (*dynamicCalculator)(nil)

	ErrCalculatingComplexity = errors.New("error calculating complexity")
	ErrCalculatingGas        = errors.New("error calculating gas")
	ErrCalculatingCost       = errors.New("error calculating cost")
)

func NewDynamicCalculator(
	weights gas.Dimensions,
	price gas.Price,
) Calculator {
	return &dynamicCalculator{
		weights: weights,
		price:   price,
	}
}

type dynamicCalculator struct {
	weights gas.Dimensions
	price   gas.Price
}

func (c *dynamicCalculator) CalculateFeeForGas(g gas.Gas) (uint64, error) {
	fee, err := g.Cost(c.price)
	if err != nil {
		return 0, fmt.Errorf(
			"%w with gas (%d) and price (%d): %w",
			ErrCalculatingCost,
			g,
			c.price,
			err,
		)
	}
	return fee, nil
}

func (c *dynamicCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	gas, err := TxGas(tx, c.weights)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCalculatingGas, err)
	}
	fee, err := gas.Cost(c.price)
	if err != nil {
		return 0, fmt.Errorf(
			"%w with gas (%d) and price (%d): %w",
			ErrCalculatingCost,
			gas,
			c.price,
			err,
		)
	}
	return fee, nil
}
