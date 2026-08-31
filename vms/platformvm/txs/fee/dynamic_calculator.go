// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
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

func (c *dynamicCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	complexity, err := TxComplexity(tx)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCalculatingComplexity, err)
	}
	return c.cost(complexity)
}

func (c *dynamicCalculator) cost(complexity gas.Dimensions) (uint64, error) {
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
			ErrCalculatingCost,
			gas,
			c.price,
			err,
		)
	}
	return fee, nil
}

// WithCredentials returns a calculator that also charges for creds, which
// [TxComplexity] cannot see: a warp credential costs an aggregate BLS
// verification, not the secp256k1 signatures the input pricing assumes.
// Calculators that predate warp credentials are returned unchanged.
func WithCredentials(c Calculator, creds []verify.Verifiable) Calculator {
	dynamic, ok := c.(*dynamicCalculator)
	if !ok {
		return c
	}
	return &credCalculator{
		dynamicCalculator: dynamic,
		creds:             creds,
	}
}

type credCalculator struct {
	*dynamicCalculator
	creds []verify.Verifiable
}

func (c *credCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	txComplexity, err := TxComplexity(tx)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCalculatingComplexity, err)
	}
	credComplexity, err := CredentialComplexity(c.creds...)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCalculatingComplexity, err)
	}
	complexity, err := txComplexity.Add(&credComplexity)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCalculatingComplexity, err)
	}
	return c.cost(complexity)
}
