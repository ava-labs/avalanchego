// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ Attestor = (*testAttestor)(nil)

func TestHandler(t *testing.T) {
	tests := []struct {
		name           string
		attestor       Attestor
		expectedErr    error
		expectedVerify bool
	}{
		{
			name:        "signature fails attestation",
			attestor:    &testAttestor{Err: errors.New("foo")},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name:        "signature not attested",
			attestor:    &testAttestor{CantAttest: true},
			expectedErr: p2p.ErrAttestFailed,
		},
		{
			name:           "signature attested",
			attestor:       &testAttestor{},
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
			h := NewHandler(tt.attestor, signer, networkID, chainID)
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

			require.NoError(c.AppRequest(ctx, set.Of(serverNodeID), requestBytes, onResponse))
			<-done
		})
	}
}

// The zero value of testAttestor attests
type testAttestor struct {
	CantAttest bool
	Err        error
}

func (t testAttestor) Attest(*warp.UnsignedMessage, []byte) (bool, error) {
	return !t.CantAttest, t.Err
}
