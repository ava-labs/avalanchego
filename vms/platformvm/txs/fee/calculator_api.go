// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Calculator is a wrapper to exposed a more user friendly API than txs.Visitor allows
// backend is not embedded to avoid exposing unnecessary methods
type Calculator struct {
	b backend
}

func (c *Calculator) GetFee() uint64 {
	return c.b.getFee()
}

func (c *Calculator) ResetFee(newFee uint64) {
	c.b.resetFee(newFee)
}

func (c *Calculator) CalculateFee(tx txs.UnsignedTx, creds []verify.Verifiable) (uint64, error) {
	return c.b.calculateFee(tx, creds)
}

func (c *Calculator) AddFeesFor(complexity fee.Dimensions) (uint64, error) {
	return c.b.addFeesFor(complexity)
}

func (c *Calculator) RemoveFeesFor(unitsToRm fee.Dimensions) (uint64, error) {
	return c.b.removeFeesFor(unitsToRm)
}

func (c *Calculator) GetGasPrice() fee.GasPrice {
	return c.b.getGasPrice()
}

func (c *Calculator) GetBlockGas() fee.Gas {
	return c.b.getBlockGas()
}

func (c *Calculator) GetGasCap() fee.Gas {
	return c.b.getGasCap()
}

func (c *Calculator) IsEActive() bool {
	return c.b.isEActive()
}

// backend is the interfaces that any fee backend must implement
type backend interface {
	txs.Visitor

	getFee() uint64
	resetFee(newFee uint64)
	addFeesFor(complexity fee.Dimensions) (uint64, error)
	removeFeesFor(unitsToRm fee.Dimensions) (uint64, error)
	getGasPrice() fee.GasPrice
	getBlockGas() fee.Gas
	getGasCap() fee.Gas
	setCredentials(creds []verify.Verifiable)
	calculateFee(tx txs.UnsignedTx, creds []verify.Verifiable) (uint64, error)
	isEActive() bool
}
