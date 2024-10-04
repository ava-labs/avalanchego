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

var _ txs.Visitor = (*WarpVerifier)(nil)

// VerifyWarpMessages verifies all warp messages in the tx. If any of the warp
// messages are invalid, an error is returned.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	tx txs.UnsignedTx,
) error {
	return tx.Visit(&WarpVerifier{
		Context:        ctx,
		NetworkID:      networkID,
		ValidatorState: validatorState,
		PChainHeight:   pChainHeight,
	})
}

type WarpVerifier struct {
	Context        context.Context
	NetworkID      uint32
	ValidatorState validators.State
	PChainHeight   uint64
}

func (*WarpVerifier) AddValidatorTx(*txs.AddValidatorTx) error {
	return nil
}

func (*WarpVerifier) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return nil
}

func (*WarpVerifier) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return nil
}

func (*WarpVerifier) CreateChainTx(*txs.CreateChainTx) error {
	return nil
}

func (*WarpVerifier) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return nil
}

func (*WarpVerifier) ImportTx(*txs.ImportTx) error {
	return nil
}

func (*WarpVerifier) ExportTx(*txs.ExportTx) error {
	return nil
}

func (*WarpVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil
}

func (*WarpVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil
}

func (*WarpVerifier) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return nil
}

func (*WarpVerifier) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return nil
}

func (*WarpVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (*WarpVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*WarpVerifier) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return nil
}

func (*WarpVerifier) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*WarpVerifier) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return nil
}

func (w *WarpVerifier) IncreaseBalanceTx(*txs.IncreaseBalanceTx) error {
	return nil
}

func (w *WarpVerifier) RegisterSubnetValidatorTx(tx *txs.RegisterSubnetValidatorTx) error {
	return w.verify(tx.Message)
}

func (w *WarpVerifier) SetSubnetValidatorWeightTx(tx *txs.SetSubnetValidatorWeightTx) error {
	return w.verify(tx.Message)
}

func (w *WarpVerifier) verify(message []byte) error {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return err
	}

	return msg.Signature.Verify(
		w.Context,
		&msg.UnsignedMessage,
		w.NetworkID,
		w.ValidatorState,
		w.PChainHeight,
		WarpQuorumNumerator,
		WarpQuorumDenominator,
	)
}
