// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestVerifyWarpMessages(t *testing.T) {
	var (
		subnetID     = ids.GenerateTestID()
		chainID      = ids.GenerateTestID()
		newValidator = func() (bls.Signer, *validators.GetValidatorOutput) {
			sk, err := localsigner.New()
			require.NoError(t, err)

			return sk, &validators.GetValidatorOutput{
				NodeID:    ids.GenerateTestNodeID(),
				PublicKey: sk.PublicKey(),
				Weight:    1,
			}
		}
		sk0, vdr0 = newValidator()
		sk1, vdr1 = newValidator()
		vdrs      = map[ids.NodeID]*validators.GetValidatorOutput{
			vdr0.NodeID: vdr0,
			vdr1.NodeID: vdr1,
		}
		state = &validatorstest.State{
			T: t,
			GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
				return subnetID, nil
			},
			GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				return vdrs, nil
			},
		}
	)

	validUnsignedWarpMessage, err := warp.NewUnsignedMessage(
		constants.UnitTestID,
		chainID,
		nil,
	)
	require.NoError(t, err)

	sig0, err := sk0.Sign(validUnsignedWarpMessage.Bytes())
	require.NoError(t, err)
	sig1, err := sk1.Sign(validUnsignedWarpMessage.Bytes())
	require.NoError(t, err)

	sig, err := bls.AggregateSignatures([]*bls.Signature{sig0, sig1})
	require.NoError(t, err)

	warpSignature := &warp.BitSetSignature{
		Signers:   set.NewBits(0, 1).Bytes(),
		Signature: [bls.SignatureLen]byte(bls.SignatureToBytes(sig)),
	}
	validWarpMessage, err := warp.NewMessage(
		validUnsignedWarpMessage,
		warpSignature,
	)
	require.NoError(t, err)

	invalidWarpMessage, err := warp.NewMessage(
		must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
			constants.UnitTestID+1,
			chainID,
			nil,
		)),
		warpSignature,
	)
	require.NoError(t, err)

	tests := []struct {
		name        string
		tx          txs.UnsignedTx
		expectedErr error
	}{
		{
			name: "AddValidatorTx",
			tx:   &txs.AddValidatorTx{},
		},
		{
			name: "AddSubnetValidatorTx",
			tx:   &txs.AddSubnetValidatorTx{},
		},
		{
			name: "AddDelegatorTx",
			tx:   &txs.AddDelegatorTx{},
		},
		{
			name: "CreateChainTx",
			tx:   &txs.CreateChainTx{},
		},
		{
			name: "CreateSubnetTx",
			tx:   &txs.CreateSubnetTx{},
		},
		{
			name: "ImportTx",
			tx:   &txs.ImportTx{},
		},
		{
			name: "ExportTx",
			tx:   &txs.ExportTx{},
		},
		{
			name: "AdvanceTimeTx",
			tx:   &txs.AdvanceTimeTx{},
		},
		{
			name: "RewardValidatorTx",
			tx:   &txs.RewardValidatorTx{},
		},
		{
			name: "RemoveSubnetValidatorTx",
			tx:   &txs.RemoveSubnetValidatorTx{},
		},
		{
			name: "TransformSubnetTx",
			tx:   &txs.TransformSubnetTx{},
		},
		{
			name: "AddPermissionlessValidatorTx",
			tx:   &txs.AddPermissionlessValidatorTx{},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			tx:   &txs.AddPermissionlessDelegatorTx{},
		},
		{
			name: "TransferSubnetOwnershipTx",
			tx:   &txs.TransferSubnetOwnershipTx{},
		},
		{
			name: "BaseTx",
			tx:   &txs.BaseTx{},
		},
		{
			name: "ConvertSubnetToL1Tx",
			tx:   &txs.ConvertSubnetToL1Tx{},
		},
		{
			name:        "RegisterL1ValidatorTx with unparsable message",
			tx:          &txs.RegisterL1ValidatorTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "RegisterL1ValidatorTx with invalid message",
			tx: &txs.RegisterL1ValidatorTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "RegisterL1ValidatorTx with valid message",
			tx: &txs.RegisterL1ValidatorTx{
				Message: validWarpMessage.Bytes(),
			},
		},
		{
			name:        "SetL1ValidatorWeightTx with unparsable message",
			tx:          &txs.SetL1ValidatorWeightTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "SetL1ValidatorWeightTx with invalid message",
			tx: &txs.SetL1ValidatorWeightTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "SetL1ValidatorWeightTx with valid message",
			tx: &txs.SetL1ValidatorWeightTx{
				Message: validWarpMessage.Bytes(),
			},
		},
		{
			name: "IncreaseL1ValidatorBalanceTx",
			tx:   &txs.IncreaseL1ValidatorBalanceTx{},
		},
		{
			name: "DisableL1ValidatorTx",
			tx:   &txs.DisableL1ValidatorTx{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyWarpMessages(
				t.Context(),
				constants.UnitTestID,
				state,
				0,
				test.tx,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}
