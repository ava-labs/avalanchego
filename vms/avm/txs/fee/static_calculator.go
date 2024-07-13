// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/fee"
)

var (
	_ Calculator  = (*staticCalculator)(nil)
	_ txs.Visitor = (*staticCalculator)(nil)

	errComplexityNotPriced = errors.New("complexity not priced")
)

func NewStaticCalculator(c StaticConfig) Calculator {
	return &staticCalculator{staticCfg: c}
}

type staticCalculator struct {
	// inputs
	staticCfg StaticConfig

	// outputs of visitor execution
	fee uint64
}

func (c *staticCalculator) CalculateFee(tx *txs.Tx) (uint64, error) {
	c.fee = 0 // zero fee among different calculateFee invocations (unlike gas which gets cumulated)
	err := tx.Unsigned.Visit(c)
	return c.fee, err
}

func (c *staticCalculator) GetFee() uint64 {
	return c.fee
}

func (c *staticCalculator) ResetFee(newFee uint64) {
	c.fee = newFee
}

func (*staticCalculator) AddFeesFor(fee.Dimensions) (uint64, error) {
	return 0, errComplexityNotPriced
}

func (*staticCalculator) RemoveFeesFor(fee.Dimensions) (uint64, error) {
	return 0, errComplexityNotPriced
}

func (*staticCalculator) GetGasPrice() fee.GasPrice { return fee.ZeroGasPrice }

func (*staticCalculator) GetBlockGas() (fee.Gas, error) { return fee.ZeroGas, nil }

func (*staticCalculator) GetGasCap() fee.Gas { return 0 }

func (*staticCalculator) setCredentials([]*fxs.FxCredential) {}

func (*staticCalculator) IsEActive() bool { return false }

func (c *staticCalculator) BaseTx(*txs.BaseTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) CreateAssetTx(*txs.CreateAssetTx) error {
	c.fee = c.staticCfg.CreateAssetTxFee
	return nil
}

func (c *staticCalculator) OperationTx(*txs.OperationTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) ImportTx(*txs.ImportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) ExportTx(*txs.ExportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}
