// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	WarpQuorumNumerator   = 67
	WarpQuorumDenominator = 100
)

var _ txs.Visitor = (*warpVerifier)(nil)

// VerifyWarpMessages verifies all warp messages in the tx. If any of the warp
// messages are invalid, an error is returned.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	tx txs.UnsignedTx,
) error {
	return tx.Visit(&warpVerifier{
		ctx:            ctx,
		networkID:      networkID,
		validatorState: validatorState,
		pChainHeight:   pChainHeight,
	})
}

type warpVerifier struct {
	ctx            context.Context
	networkID      uint32
	validatorState validators.State
	pChainHeight   uint64
}

func (*warpVerifier) AddValidatorTx(*txs.AddValidatorTx) error {
	return nil
}

func (*warpVerifier) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return nil
}

func (*warpVerifier) CreateChainTx(*txs.CreateChainTx) error {
	return nil
}

func (*warpVerifier) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return nil
}

func (*warpVerifier) ImportTx(*txs.ImportTx) error {
	return nil
}

func (*warpVerifier) ExportTx(*txs.ExportTx) error {
	return nil
}

func (*warpVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil
}

func (*warpVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil
}

func (*warpVerifier) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpVerifier) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return nil
}

func (*warpVerifier) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*warpVerifier) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return nil
}

func (w *warpVerifier) IncreaseBalanceTx(*txs.IncreaseBalanceTx) error {
	return nil
}

func (w *warpVerifier) DisableSubnetValidatorTx(*txs.DisableSubnetValidatorTx) error {
	return nil
}

func (w *warpVerifier) RegisterSubnetValidatorTx(tx *txs.RegisterSubnetValidatorTx) error {
	return w.verify(tx.Message)
}

func (w *warpVerifier) SetSubnetValidatorWeightTx(tx *txs.SetSubnetValidatorWeightTx) error {
	return w.verify(tx.Message)
}

func (w *warpVerifier) verify(message []byte) error {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return err
	}

	return msg.Signature.Verify(
		w.ctx,
		&msg.UnsignedMessage,
		w.networkID,
		w.validatorState,
		w.pChainHeight,
		WarpQuorumNumerator,
		WarpQuorumDenominator,
	)
}
