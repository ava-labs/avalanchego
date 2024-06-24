// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Calculator is a wrapper to exposed a more user friendly API than txs.Visitor allows
// backend is not embedded to avoid exposing unnecessary methods
type Calculator struct {
	b backend
}

func (c *Calculator) CalculateFee(tx txs.UnsignedTx, creds []verify.Verifiable) (uint64, error) {
	return c.b.calculateFee(tx, creds)
}

// backend is the interfaces that any fee backend must implement
type backend interface {
	txs.Visitor

	calculateFee(tx txs.UnsignedTx, creds []verify.Verifiable) (uint64, error)
}
