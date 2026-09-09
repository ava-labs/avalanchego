// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	WarpQuorumNumerator   = 67
	WarpQuorumDenominator = 100
)

var _ platform.TxVisitor = (*warpVerifier)(nil)

// VerifyWarpMessages verifies all warp messages in the tx. If any of the warp
// messages are invalid, an error is returned.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	tx platform.UnsignedTx,
) error {
	return tx.Visit(&warpVerifier{
		context:        ctx,
		networkID:      networkID,
		validatorState: validatorState,
		pChainHeight:   pChainHeight,
	})
}

type warpVerifier struct {
	context        context.Context
	networkID      uint32
	validatorState validators.State
	pChainHeight   uint64
}

func (*warpVerifier) AddValidatorTx(*platform.AddValidatorTx) error {
	return nil
}

func (*warpVerifier) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) AddDelegatorTx(*platform.AddDelegatorTx) error {
	return nil
}

func (*warpVerifier) CreateChainTx(*platform.CreateChainTx) error {
	return nil
}

func (*warpVerifier) CreateSubnetTx(*platform.CreateSubnetTx) error {
	return nil
}

func (*warpVerifier) ImportTx(*platform.ImportTx) error {
	return nil
}

func (*warpVerifier) ExportTx(*platform.ExportTx) error {
	return nil
}

func (*warpVerifier) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	return nil
}

func (*warpVerifier) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return nil
}

func (*warpVerifier) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) TransformSubnetTx(*platform.TransformSubnetTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpVerifier) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	return nil
}

func (*warpVerifier) BaseTx(*platform.BaseTx) error {
	return nil
}

func (*warpVerifier) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	return nil
}

func (*warpVerifier) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	return nil
}

func (*warpVerifier) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	return nil
}

func (w *warpVerifier) RegisterL1ValidatorTx(tx *platform.RegisterL1ValidatorTx) error {
	return w.verify(tx.Message)
}

func (w *warpVerifier) SetL1ValidatorWeightTx(tx *platform.SetL1ValidatorWeightTx) error {
	return w.verify(tx.Message)
}

func (*warpVerifier) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	return nil
}

func (*warpVerifier) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	return nil
}

func (*warpVerifier) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return nil
}

func (w *warpVerifier) verify(message []byte) error {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return err
	}

	validators, err := warp.GetCanonicalValidatorSetFromChainID(
		w.context,
		w.validatorState,
		w.pChainHeight,
		msg.SourceChainID,
	)
	if err != nil {
		return err
	}

	return msg.Signature.Verify(
		&msg.UnsignedMessage,
		w.networkID,
		validators,
		WarpQuorumNumerator,
		WarpQuorumDenominator,
	)
}
