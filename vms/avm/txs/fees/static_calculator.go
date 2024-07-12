// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "github.com/ava-labs/avalanchego/vms/avm/txs"

var (
	_ Calculator  = (*staticCalculator)(nil)
	_ txs.Visitor = (*staticCalculator)(nil)
)

func NewStaticCalculator(c StaticConfig) Calculator {
	return &staticCalculator{staticCfg: c}
}

func (c *staticCalculator) CalculateFee(tx *txs.Tx) (uint64, error) {
	c.fee = 0 // zero fee among different calculateFee invocations (unlike gas which gets cumulated)
	err := tx.Unsigned.Visit(c)
	return c.fee, err
}

type staticCalculator struct {
	// inputs
	staticCfg StaticConfig

	// outputs of visitor execution
	fee uint64
}

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
