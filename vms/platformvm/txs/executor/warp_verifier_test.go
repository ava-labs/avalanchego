// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestVerifyWarpMessages(t *testing.T) {
	var (
		subnetID     = ids.GenerateTestID()
		chainID      = ids.GenerateTestID()
		newValidator = func() (*bls.SecretKey, *validators.GetValidatorOutput) {
			sk, err := bls.NewSecretKey()
			require.NoError(t, err)

			return sk, &validators.GetValidatorOutput{
				NodeID:    ids.GenerateTestNodeID(),
				PublicKey: bls.PublicFromSecretKey(sk),
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

	var (
		sig0 = bls.Sign(sk0, validUnsignedWarpMessage.Bytes())
		sig1 = bls.Sign(sk1, validUnsignedWarpMessage.Bytes())
	)
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
			name: "ConvertSubnetTx",
			tx:   &txs.ConvertSubnetTx{},
		},
		{
			name:        "RegisterSubnetValidatorTx with unparsable message",
			tx:          &txs.RegisterSubnetValidatorTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "RegisterSubnetValidatorTx with invalid message",
			tx: &txs.RegisterSubnetValidatorTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "RegisterSubnetValidatorTx with valid message",
			tx: &txs.RegisterSubnetValidatorTx{
				Message: validWarpMessage.Bytes(),
			},
		},
		{
			name:        "SetSubnetValidatorWeightTx with unparsable message",
			tx:          &txs.SetSubnetValidatorWeightTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "SetSubnetValidatorWeightTx with invalid message",
			tx: &txs.SetSubnetValidatorWeightTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "SetSubnetValidatorWeightTx with valid message",
			tx: &txs.SetSubnetValidatorWeightTx{
				Message: validWarpMessage.Bytes(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyWarpMessages(
				context.Background(),
				constants.UnitTestID,
				state,
				0,
				test.tx,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}
