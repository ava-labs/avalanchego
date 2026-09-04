// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import "github.com/ava-labs/avalanchego/vms/platformvm/platform"

var _ Calculator = (*SimpleCalculator)(nil)

type SimpleCalculator struct {
	txFee uint64
}

func NewSimpleCalculator(fee uint64) *SimpleCalculator {
	return &SimpleCalculator{
		txFee: fee,
	}
}

func (c *SimpleCalculator) CalculateFee(platform.UnsignedTx) (uint64, error) {
	return c.txFee, nil
}
