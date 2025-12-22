// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestSignatureAggregator_AggregateSignatures(t *testing.T) {
	networkID := uint32(123)
	chainID := ids.GenerateTestID()

	nodeID0 := ids.GenerateTestNodeID()
	sk0, err := localsigner.New()
	require.NoError(t, err)
	pk0 := sk0.PublicKey()
	signer0 := warp.NewSigner(sk0, networkID, chainID)

	nodeID1 := ids.GenerateTestNodeID()
	sk1, err := localsigner.New()
	require.NoError(t, err)
	pk1 := sk1.PublicKey()
	signer1 := warp.NewSigner(sk1, networkID, chainID)

	nodeID2 := ids.GenerateTestNodeID()
	sk2, err := localsigner.New()
	require.NoError(t, err)
	pk2 := sk2.PublicKey()
	signer2 := warp.NewSigner(sk2, networkID, chainID)

	tests := []struct {
		name           string
		peers          map[ids.NodeID]p2p.Handler
		ctx            context.Context
		msg            *warp.Message
		signature      warp.BitSetSignature
		validators     []*validators.Warp
		quorumNum      uint64
		quorumDen      uint64
		wantTotalStake int
		wantSigners    int
		wantErr        error
	}{
		{
			name: "single validator - less than threshold",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer0),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
			},
			wantTotalStake: 1,
			quorumNum:      1,
			quorumDen:      1,
		},
		{
			name: "single validator - equal to threshold",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
			},
			wantTotalStake: 1,
			wantSigners:    1,
			quorumNum:      1,
			quorumDen:      1,
		},
		{
			name: "single validator - greater than threshold",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
			},
			wantTotalStake: 1,
			wantSigners:    1,
			quorumNum:      1,
			quorumDen:      2,
		},
		{
			name: "multiple validators - less than threshold - equal weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer1),
				nodeID2: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 3,
			wantSigners:    1,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "multiple validators - equal to threshold - equal weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 3,
			wantSigners:    2,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "multiple validators - greater than threshold - equal weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 3,
			wantSigners:    2,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "multiple validators - less than threshold - different weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer1),
				nodeID2: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         2,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         3,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 6,
			wantSigners:    1,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "multiple validators - equal to threshold - different weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         2,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         3,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 6,
			wantSigners:    2,
			quorumNum:      1,
			quorumDen:      2,
		},
		{
			name: "multiple validators - greater than threshold - different weights",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{Errs: []*common.AppError{common.ErrUndefined}}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         2,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         7,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 10,
			wantSigners:    2,
			quorumNum:      4,
			quorumDen:      10,
		},
		{
			name: "multiple validators - shared public keys",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer1),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{}, signer1),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0, nodeID1, nodeID2},
				},
			},
			wantTotalStake: 1,
			wantSigners:    1,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "multiple validators - unique and shared public keys",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{}, signer1),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1, nodeID2},
				},
			},
			wantTotalStake: 2,
			wantSigners:    1,
			quorumNum:      2,
			quorumDen:      3,
		},
		{
			name: "single validator - context canceled",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: p2p.NoOpHandler{},
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			}(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
			},
			wantTotalStake: 1,
			quorumNum:      1,
			quorumDen:      1,
		},
		{
			name: "multiple validators - context canceled",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: p2p.NoOpHandler{},
				nodeID1: p2p.NoOpHandler{},
				nodeID2: p2p.NoOpHandler{},
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			}(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       &warp.BitSetSignature{},
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 3,
			quorumNum:      1,
			quorumDen:      1,
		},
		{
			name: "multiple validators - resume aggregation on signature",
			peers: map[ids.NodeID]p2p.Handler{
				nodeID0: NewHandler(&testVerifier{}, signer0),
				nodeID1: NewHandler(&testVerifier{}, signer1),
				nodeID2: NewHandler(&testVerifier{}, signer2),
			},
			ctx: t.Context(),
			msg: func() *warp.Message {
				unsignedMsg, err := warp.NewUnsignedMessage(
					networkID,
					chainID,
					[]byte("payload"),
				)
				require.NoError(t, err)

				sig := &warp.BitSetSignature{
					Signers:   set.NewBits(0).Bytes(),
					Signature: [96]byte{},
				}

				sigBytes, err := signer0.Sign(unsignedMsg)
				require.NoError(t, err)
				copy(sig.Signature[:], sigBytes)

				return &warp.Message{
					UnsignedMessage: *unsignedMsg,
					Signature:       sig,
				}
			}(),
			validators: []*validators.Warp{
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
					PublicKey:      pk0,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID0},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
					PublicKey:      pk1,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID1},
				},
				{
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2),
					PublicKey:      pk2,
					Weight:         1,
					NodeIDs:        []ids.NodeID{nodeID2},
				},
			},
			wantTotalStake: 3,
			wantSigners:    3,
			quorumNum:      1,
			quorumDen:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			client := p2ptest.NewClientWithPeers(
				t,
				t.Context(),
				ids.EmptyNodeID,
				p2p.NoOpHandler{},
				tt.peers,
			)
			aggregator := NewSignatureAggregator(logging.NoLog{}, client)

			gotMsg, gotAggregatedStake, gotTotalStake, err := aggregator.AggregateSignatures(
				tt.ctx,
				tt.msg,
				[]byte("justification"),
				tt.validators,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.wantErr)

			if tt.wantErr != nil {
				return
			}

			gotSignature := gotMsg.Signature.(*warp.BitSetSignature)
			bitSet := set.BitsFromBytes(gotSignature.Signers)
			require.Equal(tt.wantSigners, bitSet.Len())

			var (
				pks                 []*bls.PublicKey
				wantAggregatedStake uint64
			)
			for i := 0; i < bitSet.BitLen(); i++ {
				if !bitSet.Contains(i) {
					continue
				}

				pks = append(pks, tt.validators[i].PublicKey)
				wantAggregatedStake += tt.validators[i].Weight
			}

			if tt.wantSigners > 0 {
				aggPk, err := bls.AggregatePublicKeys(pks)
				require.NoError(err)
				blsSig, err := bls.SignatureFromBytes(gotSignature.Signature[:])
				require.NoError(err)
				require.True(bls.Verify(aggPk, blsSig, tt.msg.UnsignedMessage.Bytes()))
			}

			require.Equal(new(big.Int).SetUint64(wantAggregatedStake), gotAggregatedStake)
			require.Equal(new(big.Int).SetUint64(uint64(tt.wantTotalStake)), gotTotalStake)
		})
	}
}
