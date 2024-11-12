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
		verifier     Verifier
		expectedErrs []error
	}{
		{
			name:   "signature fails verification",
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
			name:     "signature signed",
			cacher:   &cache.Empty[ids.ID, []byte]{},
			verifier: &testVerifier{},
			expectedErrs: []error{
				nil,
			},
		},
		{
			name: "signature is cached",
			cacher: &cache.LRU[ids.ID, []byte]{
				Size: 1,
			},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := context.Background()
			sk, err := bls.NewSecretKey()
			require.NoError(err)
			pk := bls.PublicFromSecretKey(sk)
			networkID := uint32(123)
			chainID := ids.GenerateTestID()
			signer := warp.NewSigner(sk, networkID, chainID)
			h := NewCachedHandler(tt.cacher, tt.verifier, signer)
			clientNodeID := ids.GenerateTestNodeID()
			serverNodeID := ids.GenerateTestNodeID()
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
