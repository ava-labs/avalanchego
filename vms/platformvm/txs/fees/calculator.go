// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

func (fc *Calculator) AddValidatorTx(*txs.AddValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddSubnetValidatorFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) CreateChainTx(*txs.CreateChainTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateBlockchainTxFee(fc.ChainTime)
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateSubnetTxFee(fc.ChainTime)
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (*Calculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*Calculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *Calculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TransformSubnetTxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.Config.AddSubnetValidatorFee
		} else {
			fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
		}
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.Config.AddSubnetDelegatorFee
		} else {
			fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
		}
		return nil
	}

	return errEForkFeesNotDefinedYet
}

func (fc *Calculator) BaseTx(*txs.BaseTx) error {
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
