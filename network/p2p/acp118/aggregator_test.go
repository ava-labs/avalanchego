// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
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
	chainID := ids.GenerateTestID()
	signer := warp.NewSigner(sk0, networkID, chainID)

	tests := []struct {
		name string

		handler p2p.Handler

		ctx        context.Context
		validators []Validator

		pChainState  validators.State
		pChainHeight uint64
		quorumNum    uint64
		quorumDen    uint64

		wantAggregateSignaturesErr error
		wantVerifyErr              error
	}{
		{
			name:    "gets signatures from sufficient stake",
			handler: NewHandler(&testVerifier{}, signer),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			pChainState: &validatorstest.State{
				T: t,
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return ids.Empty, nil
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
			name:    "gets signatures from insufficient stake",
			handler: NewHandler(&testVerifier{}, signer),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			pChainState: &validatorstest.State{
				T: t,
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return ids.Empty, nil
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
			name:    "overflow",
			handler: NewHandler(&testVerifier{}, signer),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    math.MaxUint[uint64](),
				},
				{
					NodeID:    nodeID1,
					PublicKey: pk1,
					Weight:    math.MaxUint[uint64](),
				},
			},
			pChainState: &validatorstest.State{
				T: t,
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return ids.Empty, nil
				},
				GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return map[ids.NodeID]*validators.GetValidatorOutput{
						nodeID0: {
							NodeID:    nodeID0,
							PublicKey: pk0,
							Weight:    math.MaxUint[uint64](),
						},
						nodeID1: {
							NodeID:    nodeID1,
							PublicKey: pk1,
							Weight:    math.MaxUint[uint64](),
						},
					}, nil
				},
			},
			quorumNum:                  1,
			quorumDen:                  2,
			wantAggregateSignaturesErr: math.ErrOverflow,
		},
		{
			name: "fails attestation",
			handler: NewHandler(
				&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
				signer,
			),
			ctx: context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			wantAggregateSignaturesErr: ErrFailedAggregation,
			quorumNum:                  1,
			quorumDen:                  1,
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
			quorumNum:                  1,
			quorumDen:                  1,
		},
		{
			name: "context canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			}(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			wantAggregateSignaturesErr: ErrFailedAggregation,
			quorumNum:                  1,
			quorumDen:                  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			unsignedMsg, err := warp.NewUnsignedMessage(networkID, chainID, []byte("payload"))
			require.NoError(err)
			msg, err := warp.NewMessage(unsignedMsg, &warp.BitSetSignature{Signature: [bls.SignatureLen]byte{}})
			require.NoError(err)
			client := p2ptest.NewClient(t, context.Background(), tt.handler, ids.GenerateTestNodeID(), nodeID0)
			aggregator := NewSignatureAggregator(logging.NoLog{}, client, 1)

			gotMsg, err := aggregator.AggregateSignatures(
				tt.ctx,
				msg,
				[]byte("justification"),
				tt.validators,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.wantAggregateSignaturesErr)

			if tt.wantAggregateSignaturesErr != nil {
				return
			}

			err = gotMsg.Signature.Verify(
				context.Background(),
				&gotMsg.UnsignedMessage,
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
