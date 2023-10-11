// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/warp"
	"github.com/stretchr/testify/require"
)

func TestSignatureHandler(t *testing.T) {
	database := memdb.New()
	snowCtx := snow.DefaultContextTest()
	blsSecretKey, err := bls.NewSecretKey()
	require.NoError(t, err)

	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)
	backend := warp.NewBackend(warpSigner, database, 100)

	msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, []byte("test"))
	require.NoError(t, err)

	messageID := msg.ID()
	require.NoError(t, backend.AddMessage(msg))
	signature, err := backend.GetSignature(messageID)
	require.NoError(t, err)
	unknownMessageID := ids.GenerateTestID()

	emptySignature := [bls.SignatureLen]byte{}

	tests := map[string]struct {
		setup       func() (request message.SignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *handlerStats)
	}{
		"normal": {
			setup: func() (request message.SignatureRequest, expectedResponse []byte) {
				return message.SignatureRequest{
					MessageID: messageID,
				}, signature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.signatureRequest.Count())
				require.EqualValues(t, 1, stats.signatureHit.Count())
				require.EqualValues(t, 0, stats.signatureMiss.Count())
				require.Greater(t, stats.signatureRequestDuration.Value(), time.Duration(0))
			},
		},
		"unknown": {
			setup: func() (request message.SignatureRequest, expectedResponse []byte) {
				return message.SignatureRequest{
					MessageID: unknownMessageID,
				}, emptySignature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.signatureRequest.Count())
				require.EqualValues(t, 0, stats.signatureHit.Count())
				require.EqualValues(t, 1, stats.signatureMiss.Count())
				require.Greater(t, stats.signatureRequestDuration.Value(), time.Duration(0))
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewSignatureRequestHandler(backend, message.Codec)
			handler.stats.Clear()

			request, expectedResponse := test.setup()
			responseBytes, err := handler.OnSignatureRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
			require.NoError(t, err)

			test.verifyStats(t, handler.stats)

			// If the expected response is empty, assert that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				require.Len(t, responseBytes, 0, "expected response to be empty")
				return
			}
			var response message.SignatureResponse
			_, err = message.Codec.Unmarshal(responseBytes, &response)
			require.NoError(t, err, "error unmarshalling SignatureResponse")

			require.Equal(t, expectedResponse, response.Signature[:])
		})
	}
}
