// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
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

	nodeID2 := ids.GenerateTestNodeID()
	sk2, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk2 := bls.PublicFromSecretKey(sk2)

	networkID := uint32(123)
	chainID := ids.GenerateTestID()
	signer := warp.NewSigner(sk0, networkID, chainID)

	tests := []struct {
		name                       string
		handler                    p2p.Handler
		ctx                        context.Context
		validators                 []Validator
		quorumNum                  uint64
		quorumDen                  uint64
		wantMsg                    *warp.Message
		wantSigners                []int
		wantAggregateSignaturesErr error
	}{
		{
			name:    "aggregates from all validators",
			handler: NewHandler(&testVerifier{}, signer),
			ctx:     context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
			},
			wantSigners: []int{0},
			quorumNum:   1,
			quorumDen:   1,
		},
		{
			name: "fails aggregation from some validators - 1/2",
			handler: NewHandler(
				&testVerifier{
					Errs: []*common.AppError{nil, common.ErrUndefined},
				},
				signer,
			),
			ctx: context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
				{
					NodeID:    nodeID1,
					PublicKey: pk1,
					Weight:    1,
				},
			},
			quorumNum:                  1,
			quorumDen:                  1,
			wantSigners:                []int{0},
			wantAggregateSignaturesErr: ErrFailedAggregation,
		},
		{
			name: "fails aggregation from some validators - 2/3",
			handler: NewHandler(
				&testVerifier{
					Errs: []*common.AppError{nil, nil, common.ErrUndefined},
				},
				signer,
			),
			ctx: context.Background(),
			validators: []Validator{
				{
					NodeID:    nodeID0,
					PublicKey: pk0,
					Weight:    1,
				},
				{
					NodeID:    nodeID1,
					PublicKey: pk1,
					Weight:    1,
				},
				{
					NodeID:    nodeID2,
					PublicKey: pk2,
					Weight:    1,
				},
			},
			quorumNum:                  1,
			quorumDen:                  1,
			wantSigners:                []int{0, 1},
			wantAggregateSignaturesErr: ErrFailedAggregation,
		},
		{
			name: "fails aggregation from all validators",
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
			name:    "context canceled",
			handler: NewHandler(&testVerifier{}, signer),
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
			quorumNum: 0,
			quorumDen: 1,
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
			aggregator := NewSignatureAggregator(logging.NoLog{}, client)

			msg, gotNum, gotDen, err := aggregator.AggregateSignatures(
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

			bitSetSignature := msg.Signature.(*warp.BitSetSignature)
			require.Len(bitSetSignature.Signers, len(tt.wantSigners))

			wantNum := uint64(0)
			bitSet := set.BitsFromBytes(bitSetSignature.Signers)
			for _, i := range tt.wantSigners {
				require.True(bitSet.Contains(i))
				wantNum += tt.validators[i].Weight
			}

			wantDen := uint64(0)
			for _, v := range tt.validators {
				wantDen += v.Weight
			}

			require.Equal(wantNum, gotNum)
			require.Equal(wantDen, gotDen)
		})
	}
}
