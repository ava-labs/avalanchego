// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ Verifier = (*testVerifier)(nil)

func TestHandler(t *testing.T) {
	tests := []struct {
		name         string
		msg          *warp.UnsignedMessage
		cacher       cache.Cacher[ids.ID, []byte]
		verifier     Verifier
		expectedErrs []error
	}{
		{
			name: "signature fails verification",
			msg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(
					uint32(123),
					ids.ID{1, 2, 3},
					[]byte("payload"),
				)
				require.NoError(t, err)

				return msg
			}(),
			cacher: &cache.Empty[ids.ID, []byte]{},
			verifier: &testVerifier{
				Errs: []*common.AppError{
					{Code: 123},
				},
			},
			expectedErrs: []error{
				&common.AppError{Code: 123},
			},
		},
		{
			name: "signature signed",
			msg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(
					uint32(123),
					ids.ID{1, 2, 3},
					[]byte("payload"),
				)
				require.NoError(t, err)

				return msg
			}(),
			cacher:   &cache.Empty[ids.ID, []byte]{},
			verifier: &testVerifier{},
			expectedErrs: []error{
				nil,
			},
		},
		{
			name: "signature is cached",
			msg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(
					uint32(123),
					ids.ID{1, 2, 3},
					[]byte("payload"),
				)
				require.NoError(t, err)

				return msg
			}(),
			cacher: lru.NewCache[ids.ID, []byte](1),
			verifier: &testVerifier{
				Errs: []*common.AppError{
					nil,
					{Code: 123}, // The valid response should be cached
				},
			},
			expectedErrs: []error{
				nil,
				nil,
			},
		},
		{
			name: "invalid network id",
			msg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(
					uint32(456),
					ids.ID{1, 2, 3},
					[]byte("payload"),
				)
				require.NoError(t, err)

				return msg
			}(),
			cacher:   &cache.Empty[ids.ID, []byte]{},
			verifier: &testVerifier{},
			expectedErrs: []error{
				p2p.ErrUnexpected,
			},
		},
		{
			name: "invalid chain id",
			msg: func() *warp.UnsignedMessage {
				msg, err := warp.NewUnsignedMessage(
					uint32(123),
					ids.ID{4, 5, 6},
					[]byte("payload"),
				)
				require.NoError(t, err)

				return msg
			}(),
			cacher:   &cache.Empty[ids.ID, []byte]{},
			verifier: &testVerifier{},
			expectedErrs: []error{
				p2p.ErrUnexpected,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := t.Context()
			sk, err := localsigner.New()
			require.NoError(err)
			pk := sk.PublicKey()
			networkID := uint32(123)
			chainID := ids.ID{1, 2, 3}
			signer := warp.NewSigner(sk, networkID, chainID)
			h := NewCachedHandler(tt.cacher, tt.verifier, signer)
			clientNodeID := ids.GenerateTestNodeID()
			serverNodeID := ids.GenerateTestNodeID()
			c := p2ptest.NewClient(
				t,
				ctx,
				clientNodeID,
				p2p.NoOpHandler{},
				serverNodeID,
				h,
			)

			request := &sdk.SignatureRequest{
				Message:       tt.msg.Bytes(),
				Justification: []byte("justification"),
			}

			requestBytes, err := proto.Marshal(request)
			require.NoError(err)

			var (
				expectedErr error
				handled     = make(chan struct{})
			)
			onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
				defer func() {
					handled <- struct{}{}
				}()

				require.ErrorIs(appErr, expectedErr)
				if appErr != nil {
					return
				}

				response := &sdk.SignatureResponse{}
				require.NoError(proto.Unmarshal(responseBytes, response))

				signature, err := bls.SignatureFromBytes(response.Signature)
				require.NoError(err)

				require.True(bls.Verify(pk, signature, request.Message))

				// Ensure the cache is populated with correct signature
				sig, ok := tt.cacher.Get(tt.msg.ID())
				if ok {
					require.Equal(sig, response.Signature)
				}
			}

			for _, expectedErr = range tt.expectedErrs {
				require.NoError(c.AppRequest(ctx, set.Of(serverNodeID), requestBytes, onResponse))
				<-handled
			}
		})
	}
}

// The zero value of testVerifier allows signing
type testVerifier struct {
	Errs []*common.AppError
}

func (t *testVerifier) Verify(
	context.Context,
	*warp.UnsignedMessage,
	[]byte,
) *common.AppError {
	if len(t.Errs) == 0 {
		return nil
	}
	err := t.Errs[0]
	t.Errs = t.Errs[1:]
	return err
}
