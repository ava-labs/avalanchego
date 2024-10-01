// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

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
		name           string
		verifier       Verifier
		expectedErr    error
		expectedVerify bool
	}{
		{
			name:        "signature fails verification",
			verifier:    &testVerifier{Err: &common.AppError{Code: 123}},
			expectedErr: &common.AppError{Code: 123},
		},
		{
			name:           "signature signed",
			verifier:       &testVerifier{},
			expectedVerify: true,
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
			h := NewHandler(tt.verifier, signer)
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

			done := make(chan struct{})
			onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
				defer close(done)

				if appErr != nil {
					require.ErrorIs(tt.expectedErr, appErr)
					return
				}

				response := &sdk.SignatureResponse{}
				require.NoError(proto.Unmarshal(responseBytes, response))

				signature, err := bls.SignatureFromBytes(response.Signature)
				require.NoError(err)

				require.Equal(tt.expectedVerify, bls.Verify(pk, signature, request.Message))
			}

			require.NoError(c.AppRequest(ctx, set.Of(clientNodeID), requestBytes, onResponse))
			<-done
		})
	}
}

// The zero value of testVerifier allows signing
type testVerifier struct {
	Err *common.AppError
}

func (t testVerifier) Verify(
	context.Context,
	*warp.UnsignedMessage,
	[]byte,
) *common.AppError {
	return t.Err
}
