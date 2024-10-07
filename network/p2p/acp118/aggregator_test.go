// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestVerifier_Verify(t *testing.T) {
	nodeID0 := ids.GenerateTestNodeID()
	sk0, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk0 := bls.PublicFromSecretKey(sk0)

	nodeID1 := ids.GenerateTestNodeID()
	sk1, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk1 := bls.PublicFromSecretKey(sk1)

	networkID := uint32(123)
	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	signer := warp.NewSigner(sk0, networkID, chainID)

	tests := []struct {
		name string

		handler p2p.Handler

		ctx        context.Context
		validators []Validator
		threshold  uint64

		pChainState  validators.State
		pChainHeight uint64
		quorumNum    uint64
		quorumDen    uint64

		wantAggregateSignaturesErr error
		wantVerifyErr              error
	}{
		{
			name:    "passes attestation and verification",
			handler: NewHandler(&testAttestor{}, signer, networkID, chainID),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			threshold: 1,
			pChainState: &validatorstest.State{
				T: t,
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return subnetID, nil
				},
				GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return map[ids.NodeID]*validators.GetValidatorOutput{
						nodeID0: {
							NodeID:    nodeID0,
							PublicKey: pk0,
							Weight:    1,
						},
					}, nil
				},
			},
			quorumNum: 1,
			quorumDen: 1,
		},
		{
			name:    "passes attestation and fails verification - insufficient stake",
			handler: NewHandler(&testAttestor{}, signer, networkID, chainID),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			threshold: 1,
			pChainState: &validatorstest.State{
				T: t,
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return subnetID, nil
				},
				GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return map[ids.NodeID]*validators.GetValidatorOutput{
						nodeID0: {
							NodeID:    nodeID0,
							PublicKey: pk0,
							Weight:    1,
						},
						nodeID1: {
							NodeID:    nodeID1,
							PublicKey: pk1,
							Weight:    1,
						},
					}, nil
				},
			},
			quorumNum:     2,
			quorumDen:     2,
			wantVerifyErr: warp.ErrInsufficientWeight,
		},
		{
			name: "fails attestation",
			handler: NewHandler(
				&testAttestor{Err: errors.New("foobar")},
				signer,
				networkID,
				chainID,
			),
			ctx: context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			threshold:                  1,
			wantAggregateSignaturesErr: ErrInsufficientSignatures,
		},
		{
			name: "invalid validator set",
			ctx:  context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			wantAggregateSignaturesErr: ErrDuplicateValidator,
		},
		{
			name: "context canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			}(),
			wantAggregateSignaturesErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			message, err := warp.NewUnsignedMessage(networkID, chainID, []byte("payload"))
			require.NoError(err)
			client := p2ptest.NewClient(t, ctx, tt.handler, ids.GenerateTestNodeID(), nodeID0)
			verifier := NewSignatureAggregator(logging.NoLog{}, client, 1)

			signedMessage, err := verifier.AggregateSignatures(
				tt.ctx,
				message,
				[]byte("justification"),
				tt.validators,
				tt.threshold,
			)
			require.ErrorIs(err, tt.wantAggregateSignaturesErr)

			if signedMessage == nil {
				return
			}

			err = signedMessage.Signature.Verify(
				ctx,
				&signedMessage.UnsignedMessage,
				networkID,
				tt.pChainState,
				0,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.wantVerifyErr)
		})
	}
}
