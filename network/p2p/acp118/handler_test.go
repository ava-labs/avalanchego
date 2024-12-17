// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ Verifier = (*testVerifier)(nil)

func TestHandler(t *testing.T) {
	tests := []struct {
		name         string
		cacher       cache.Cacher[ids.ID, []byte]
		verifierErrs []*common.AppError
		expectedErrs []error
	}{
		{
			name:   "signature fails verification",
			cacher: &cache.Empty[ids.ID, []byte]{},
			verifierErrs: []*common.AppError{
				{Code: 123},
			},
			expectedErrs: []error{
				&common.AppError{Code: 123},
			},
		},
		{
			name:         "signature signed",
			cacher:       &cache.Empty[ids.ID, []byte]{},
			verifierErrs: nil,
			expectedErrs: []error{
				nil,
			},
		},
		{
			name: "signature is cached",
			cacher: &cache.LRU[ids.ID, []byte]{
				Size: 1,
			},
			verifierErrs: []*common.AppError{
				nil,
				{Code: 123}, // The valid response should be cached
			},
			expectedErrs: []error{
				nil,
				nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			sk, err := bls.NewSigner()
			require.NoError(err)
			pk := sk.PublicKey()
			networkID := uint32(123)
			chainID := ids.GenerateTestID()
			signer := warp.NewSigner(sk, networkID, chainID)
			clientNodeID := ids.GenerateTestNodeID()
			serverNodeID := ids.GenerateTestNodeID()
			h := NewCachedHandler(tt.cacher, newTestVerifier(t, clientNodeID, tt.verifierErrs), signer)

			c := p2ptest.NewClient(
				t,
				ctx,
				h,
				clientNodeID,
				serverNodeID,
			)

			unsignedMessage, err := warp.NewUnsignedMessage(
				networkID,
				chainID,
				[]byte("payload"),
			)
			require.NoError(err)

			request := &sdk.SignatureRequest{
				Message:       unsignedMessage.Bytes(),
				Justification: []byte("justification"),
			}

			requestBytes, err := proto.Marshal(request)
			require.NoError(err)

			var (
				expectedErr error
				handled     = make(chan struct{})
			)
			onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, appErr error) {
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
				sig, ok := tt.cacher.Get(unsignedMessage.ID())
				if ok {
					require.Equal(sig, response.Signature)
				}

				require.Equal(serverNodeID, nodeID)
			}

			for _, expectedErr = range tt.expectedErrs {
				require.NoError(c.AppRequest(ctx, set.Of(clientNodeID), requestBytes, onResponse))
				<-handled
			}
		})
	}
}

// The zero value of testVerifier allows signing
type testVerifier struct {
	t            *testing.T
	errs         []*common.AppError
	clientNodeID ids.NodeID
}

func newTestVerifier(t *testing.T, clientNodeID ids.NodeID, errs []*common.AppError) *testVerifier {
	return &testVerifier{
		t:            t,
		errs:         errs,
		clientNodeID: clientNodeID,
	}
}

func (t *testVerifier) Verify(
	_ context.Context,
	_ *warp.UnsignedMessage,
	justification []byte,
	nodeID ids.NodeID,
) *common.AppError {
	require.Equal(t.t, t.clientNodeID, nodeID)
	require.Equal(t.t, []byte("justification"), justification)
	if len(t.errs) == 0 {
		return nil
	}
	err := t.errs[0]
	t.errs = t.errs[1:]
	return err
}
