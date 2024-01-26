// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var (
	_ txs.Visitor = (*Calculator)(nil)

	errEForkFeesNotDefinedYet = errors.New("fees in E fork not defined yet")
)

type Calculator struct {
	// setup, to be filled before visitor methods are called
	Config    *config.Config
	ChainTime time.Time

	// outputs of visitor execution
	Fee uint64
}

func (fc *Calculator) BaseTx(*txs.BaseTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) CreateAssetTx(*txs.CreateAssetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.CreateAssetTxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) OperationTx(*txs.OperationTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) ImportTx(*txs.ImportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) ExportTx(*txs.ExportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}
