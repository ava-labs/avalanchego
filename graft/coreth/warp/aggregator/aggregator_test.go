// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func newValidator(t testing.TB, weight uint64) (bls.Signer, *avalancheWarp.Validator) {
	sk, err := localsigner.New()
	require.NoError(t, err)
	pk := sk.PublicKey()
	return sk, &avalancheWarp.Validator{
		PublicKey:      pk,
		PublicKeyBytes: bls.PublicKeyToCompressedBytes(pk),
		Weight:         weight,
		NodeIDs:        []ids.NodeID{ids.GenerateTestNodeID()},
	}
}

func TestAggregateSignatures(t *testing.T) {
	errTest := errors.New("test error")
	unsignedMsg := &avalancheWarp.UnsignedMessage{
		NetworkID:     1338,
		SourceChainID: ids.ID{'y', 'e', 'e', 't'},
		Payload:       []byte("hello world"),
	}
	require.NoError(t, unsignedMsg.Initialize())

	nodeID1, nodeID2, nodeID3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	vdrWeight := uint64(10001)
	vdr1sk, vdr1 := newValidator(t, vdrWeight)
	vdr2sk, vdr2 := newValidator(t, vdrWeight+1)
	vdr3sk, vdr3 := newValidator(t, vdrWeight-1)
	sig1, err := vdr1sk.Sign(unsignedMsg.Bytes())
	require.NoError(t, err)
	sig2, err := vdr2sk.Sign(unsignedMsg.Bytes())
	require.NoError(t, err)
	sig3, err := vdr3sk.Sign(unsignedMsg.Bytes())
	require.NoError(t, err)
	vdrToSig := map[*avalancheWarp.Validator]*bls.Signature{
		vdr1: sig1,
		vdr2: sig2,
		vdr3: sig3,
	}
	nonVdrSk, err := localsigner.New()
	require.NoError(t, err)
	nonVdrSig, err := nonVdrSk.Sign(unsignedMsg.Bytes())
	require.NoError(t, err)
	vdrs := []*avalancheWarp.Validator{
		{
			PublicKey: vdr1.PublicKey,
			NodeIDs:   []ids.NodeID{nodeID1},
			Weight:    vdr1.Weight,
		},
		{
			PublicKey: vdr2.PublicKey,
			NodeIDs:   []ids.NodeID{nodeID2},
			Weight:    vdr2.Weight,
		},
		{
			PublicKey: vdr3.PublicKey,
			NodeIDs:   []ids.NodeID{nodeID3},
			Weight:    vdr3.Weight,
		},
	}

	type test struct {
		name                  string
		contextWithCancelFunc func() (context.Context, context.CancelFunc)
		aggregatorFunc        func(*gomock.Controller, context.CancelFunc) *Aggregator
		unsignedMsg           *avalancheWarp.UnsignedMessage
		quorumNum             uint64
		expectedSigners       []*avalancheWarp.Validator
		expectedErr           error
	}

	tests := []test{
		{
			name: "0/3 validators reply with signature",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errTest).Times(len(vdrs))
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg: unsignedMsg,
			quorumNum:   1,
			expectedErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "1/3 validators reply with signature; insufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(sig1, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(nil, errTest).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(nil, errTest).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg: unsignedMsg,
			quorumNum:   35, // Require >1/3 of weight
			expectedErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "2/3 validators reply with signature; insufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(sig1, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(sig2, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(nil, errTest).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg: unsignedMsg,
			quorumNum:   69, // Require >2/3 of weight
			expectedErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "2/3 validators reply with signature; sufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(sig1, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(sig2, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(nil, errTest).MaxTimes(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       65, // Require <2/3 of weight
			expectedSigners: []*avalancheWarp.Validator{vdr1, vdr2},
			expectedErr:     nil,
		},
		{
			name: "3/3 validators reply with signature; sufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(sig1, nil).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(sig2, nil).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(sig3, nil).MaxTimes(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       100, // Require all weight
			expectedSigners: []*avalancheWarp.Validator{vdr1, vdr2, vdr3},
			expectedErr:     nil,
		},
		{
			name: "3/3 validators reply with signature; 1 invalid signature; sufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(nonVdrSig, nil).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(sig2, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(sig3, nil).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       64,
			expectedSigners: []*avalancheWarp.Validator{vdr2, vdr3},
			expectedErr:     nil,
		},
		{
			name: "3/3 validators reply with signature; 3 invalid signatures; insufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(nonVdrSig, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(nonVdrSig, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(nonVdrSig, nil).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg: unsignedMsg,
			quorumNum:   1,
			expectedErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "3/3 validators reply with signature; 2 invalid signatures; insufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(nonVdrSig, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(nonVdrSig, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(sig3, nil).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg: unsignedMsg,
			quorumNum:   40,
			expectedErr: avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "1/3 validators reply with signature; 1 invalid signature; 1 error; sufficient weight",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(nonVdrSig, nil).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(nil, errTest).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).Return(sig3, nil).Times(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       30,
			expectedSigners: []*avalancheWarp.Validator{vdr3},
			expectedErr:     nil,
		},
		{
			name: "early termination of signature fetching on parent context cancellation",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				// Assert that the context passed into each goroutine is canceled
				// because the parent context is canceled.
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).MaxTimes(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       60, // Require 2/3 validators
			expectedSigners: []*avalancheWarp.Validator{vdr1, vdr2},
			expectedErr:     avalancheWarp.ErrInsufficientWeight,
		},
		{
			name: "context cancels halfway through signature fetching",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				return ctx, cancel
			},
			aggregatorFunc: func(ctrl *gomock.Controller, cancel context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						// cancel the context and return the signature
						cancel()
						return sig1, nil
					},
				).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						// Should not be able to grab another signature since context was cancelled in another go routine
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).MaxTimes(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						// Should not be able to grab another signature since context was cancelled in another go routine
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).MaxTimes(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       33, // 1/3 Should have gotten one signature before cancellation
			expectedSigners: []*avalancheWarp.Validator{vdr1},
			expectedErr:     nil,
		},
		{
			name: "early termination of signature fetching on passing threshold",
			contextWithCancelFunc: func() (context.Context, context.CancelFunc) {
				return context.Background(), nil
			},
			aggregatorFunc: func(ctrl *gomock.Controller, _ context.CancelFunc) *Aggregator {
				client := NewMockSignatureGetter(ctrl)
				client.EXPECT().GetSignature(gomock.Any(), nodeID1, gomock.Any()).Return(sig1, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID2, gomock.Any()).Return(sig2, nil).Times(1)
				client.EXPECT().GetSignature(gomock.Any(), nodeID3, gomock.Any()).DoAndReturn(
					// The aggregator will receive sig1 and sig2 which is sufficient weight,
					// so the remaining outstanding goroutine should be cancelled.
					func(ctx context.Context, _ ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
						<-ctx.Done()
						err := ctx.Err()
						require.ErrorIs(t, err, context.Canceled)
						return nil, err
					},
				).MaxTimes(1)
				return New(client, vdrs, vdrWeight*uint64(len(vdrs)))
			},
			unsignedMsg:     unsignedMsg,
			quorumNum:       60, // Require 2/3 validators
			expectedSigners: []*avalancheWarp.Validator{vdr1, vdr2},
			expectedErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			require := require.New(t)

			ctx, cancel := tt.contextWithCancelFunc()
			// Guarantees that cancel is called preventing goroutine leak
			if cancel != nil {
				defer cancel()
			}
			a := tt.aggregatorFunc(ctrl, cancel)

			res, err := a.AggregateSignatures(ctx, tt.unsignedMsg, tt.quorumNum)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				return
			}

			require.Equal(unsignedMsg, &res.Message.UnsignedMessage)

			expectedSigWeight := uint64(0)
			for _, vdr := range tt.expectedSigners {
				expectedSigWeight += vdr.Weight
			}
			require.Equal(expectedSigWeight, res.SignatureWeight)
			require.Equal(vdr1.Weight+vdr2.Weight+vdr3.Weight, res.TotalWeight)

			expectedSigs := []*bls.Signature{}
			for _, vdr := range tt.expectedSigners {
				expectedSigs = append(expectedSigs, vdrToSig[vdr])
			}
			expectedSig, err := bls.AggregateSignatures(expectedSigs)
			require.NoError(err)
			gotBLSSig, ok := res.Message.Signature.(*avalancheWarp.BitSetSignature)
			require.True(ok)
			require.Equal(bls.SignatureToBytes(expectedSig), gotBLSSig.Signature[:])

			numSigners, err := res.Message.Signature.NumSigners()
			require.NoError(err)
			require.Len(tt.expectedSigners, numSigners)
		})
	}
}
