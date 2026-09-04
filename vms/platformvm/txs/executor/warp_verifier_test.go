// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
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
		tx          platform.UnsignedTx
		expectedErr error
	}{
		{
			name: "AddValidatorTx",
			tx:   &platform.AddValidatorTx{},
		},
		{
			name: "AddSubnetValidatorTx",
			tx:   &platform.AddSubnetValidatorTx{},
		},
		{
			name: "AddDelegatorTx",
			tx:   &platform.AddDelegatorTx{},
		},
		{
			name: "CreateChainTx",
			tx:   &platform.CreateChainTx{},
		},
		{
			name: "CreateSubnetTx",
			tx:   &platform.CreateSubnetTx{},
		},
		{
			name: "ImportTx",
			tx:   &platform.ImportTx{},
		},
		{
			name: "ExportTx",
			tx:   &platform.ExportTx{},
		},
		{
			name: "AdvanceTimeTx",
			tx:   &platform.AdvanceTimeTx{},
		},
		{
			name: "RewardValidatorTx",
			tx:   &platform.RewardValidatorTx{},
		},
		{
			name: "RemoveSubnetValidatorTx",
			tx:   &platform.RemoveSubnetValidatorTx{},
		},
		{
			name: "TransformSubnetTx",
			tx:   &platform.TransformSubnetTx{},
		},
		{
			name: "AddPermissionlessValidatorTx",
			tx:   &platform.AddPermissionlessValidatorTx{},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			tx:   &platform.AddPermissionlessDelegatorTx{},
		},
		{
			name: "TransferSubnetOwnershipTx",
			tx:   &platform.TransferSubnetOwnershipTx{},
		},
		{
			name: "BaseTx",
			tx:   &platform.BaseTx{},
		},
		{
			name: "ConvertSubnetToL1Tx",
			tx:   &platform.ConvertSubnetToL1Tx{},
		},
		{
			name:        "RegisterL1ValidatorTx with unparsable message",
			tx:          &platform.RegisterL1ValidatorTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "RegisterL1ValidatorTx with invalid message",
			tx: &platform.RegisterL1ValidatorTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "RegisterL1ValidatorTx with valid message",
			tx: &platform.RegisterL1ValidatorTx{
				Message: validWarpMessage.Bytes(),
			},
		},
		{
			name:        "SetL1ValidatorWeightTx with unparsable message",
			tx:          &platform.SetL1ValidatorWeightTx{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "SetL1ValidatorWeightTx with invalid message",
			tx: &platform.SetL1ValidatorWeightTx{
				Message: invalidWarpMessage.Bytes(),
			},
			expectedErr: warp.ErrWrongNetworkID,
		},
		{
			name: "SetL1ValidatorWeightTx with valid message",
			tx: &platform.SetL1ValidatorWeightTx{
				Message: validWarpMessage.Bytes(),
			},
		},
		{
			name: "IncreaseL1ValidatorBalanceTx",
			tx:   &platform.IncreaseL1ValidatorBalanceTx{},
		},
		{
			name: "DisableL1ValidatorTx",
			tx:   &platform.DisableL1ValidatorTx{},
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
