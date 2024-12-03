// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestSignatureAggregator_AggregateSignatures(t *testing.T) {
	networkID := uint32(123)
	chainID := ids.GenerateTestID()

	nodeID0 := ids.GenerateTestNodeID()
	sk0, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk0 := bls.PublicFromSecretKey(sk0)
	signer0 := warp.NewSigner(sk0, networkID, chainID)

	nodeID1 := ids.GenerateTestNodeID()
	sk1, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk1 := bls.PublicFromSecretKey(sk1)
	signer1 := warp.NewSigner(sk1, networkID, chainID)

	nodeID2 := ids.GenerateTestNodeID()
	sk2, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk2 := bls.PublicFromSecretKey(sk2)
	signer2 := warp.NewSigner(sk2, networkID, chainID)

	tests := []struct {
		name         string
		peers        map[ids.NodeID]p2p.Handler
		ctx          context.Context
		validators   []*warp.Validator
		quorumNum    uint64
		quorumDen    uint64
		wantMsg      *warp.Message
		wantSigners  []int
		wantFinished bool
		wantErr      error
	}{
		{
			name: "aggregates from all validators 1/1",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
			},
			wantSigners:  []int{0},
			wantFinished: true,
			quorumNum:    1,
			quorumDen:    1,
		},
		{
			name: "aggregates from some validators - 1/3",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer1,
				),
				nodeID2: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer2,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
				{
					PublicKey: pk2,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID2},
				},
			},
			wantSigners:  []int{0},
			wantFinished: true,
			quorumNum:    1,
			quorumDen:    3,
		},
		{
			name: "aggregates from some validators - 2/3",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer2,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    2,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
				{
					PublicKey: pk2,
					Weight:    3,
					NodeIDs:   []ids.NodeID{nodeID2},
				},
			},
			wantSigners:  []int{0, 1},
			wantFinished: true,
			quorumNum:    3,
			quorumDen:    6,
		},
		{
			name: "aggregates from all validators - 3/3",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{}, signer2),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    2,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
				{
					PublicKey: pk2,
					Weight:    3,
					NodeIDs:   []ids.NodeID{nodeID2},
				},
			},
			wantSigners:  []int{0, 1, 2},
			wantFinished: true,
			quorumNum:    1,
			quorumDen:    1,
		},
		{
			name: "fails aggregation from some validators - 1/2",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer1,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
			},
			quorumNum:    1,
			quorumDen:    1,
			wantSigners:  []int{0},
			wantFinished: true,
		},
		{
			name: "fails aggregation from some validators - 1/3",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer1,
				),
				nodeID2: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer2,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
				{
					PublicKey: pk2,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID2},
				},
			},
			quorumNum:    2,
			quorumDen:    3,
			wantSigners:  []int{0},
			wantFinished: true,
		},
		{
			name: "fails aggregation from some validators - 2/3",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer2,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
				{
					PublicKey: pk1,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID1},
				},
				{
					PublicKey: pk2,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID2},
				},
			},
			quorumNum:    1,
			quorumDen:    1,
			wantSigners:  []int{0, 1},
			wantFinished: true,
		},
		{
			name: "fails aggregation from all validators",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(
					&testVerifier{Errs: []*common.AppError{common.ErrUndefined}},
					signer0,
				),
			},
			ctx: context.Background(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
			},
			wantFinished: true,
			quorumNum:    1,
			quorumDen:    1,
		},
		{
			name: "context canceled",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			}(),
			validators: []*warp.Validator{
				{
					PublicKey: pk0,
					Weight:    1,
					NodeIDs:   []ids.NodeID{nodeID0},
				},
			},
			quorumNum: 0,
			quorumDen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			client := p2ptest.NewClientWithPeers(
				t,
				context.Background(),
				ids.EmptyNodeID,
				p2p.NoOpHandler{},
				tt.peers,
			)
			aggregator := NewSignatureAggregator(logging.NoLog{}, client)

			unsignedMsg, err := warp.NewUnsignedMessage(
				networkID,
				chainID,
				[]byte("payload"),
			)
			require.NoError(err)
			msg, err := warp.NewMessage(
				unsignedMsg,
				&warp.BitSetSignature{Signature: [bls.SignatureLen]byte{}},
			)
			require.NoError(err)
			gotMsg, gotAggregatedStake, gotTotalStake, finished, err := aggregator.AggregateSignatures(
				tt.ctx,
				msg,
				[]byte("justification"),
				tt.validators,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantFinished, finished)

			if tt.wantErr != nil {
				return
			}

			bitSetSignature := gotMsg.Signature.(*warp.BitSetSignature)
			bitSet := set.BitsFromBytes(bitSetSignature.Signers)
			require.Equal(len(tt.wantSigners), bitSet.Len())

			wantAggregatedStake := new(big.Int)
			for _, i := range tt.wantSigners {
				require.True(bitSet.Contains(i))
				wantAggregatedStake.Add(wantAggregatedStake, new(big.Int).SetUint64(tt.validators[i].Weight))
			}

			wantTotalStake := new(big.Int)
			for _, v := range tt.validators {
				wantTotalStake.Add(wantTotalStake, new(big.Int).SetUint64(v.Weight))
			}

			require.Equal(wantAggregatedStake, gotAggregatedStake)
			require.Equal(wantTotalStake, gotTotalStake)
		})
	}
}
