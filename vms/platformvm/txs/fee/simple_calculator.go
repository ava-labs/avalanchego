// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Calculator = (*SimpleCalculator)(nil)

type SimpleCalculator struct {
	txFee uint64
}

func NewSimpleCalculator(fee uint64) *SimpleCalculator {
	return &SimpleCalculator{
		txFee: fee,
	}
}

func (c *SimpleCalculator) CalculateFee(txs.UnsignedTx) (uint64, error) {
	return c.txFee, nil
}

// CalculateFeeForGas ignores gas: pre-Etna fees are flat.
func (c *SimpleCalculator) CalculateFeeForGas(gas.Gas) (uint64, error) {
	return c.txFee, nil
}
