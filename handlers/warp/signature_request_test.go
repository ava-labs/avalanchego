// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/teleporter"
	"github.com/ava-labs/subnet-evm/handlers/stats"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/plugin/evm/warp"
	"github.com/stretchr/testify/require"
)

func TestSignatureHandler(t *testing.T) {
	database := memdb.New()
	snowCtx := snow.DefaultContextTest()
	blsSecretKey, err := bls.NewSecretKey()
	require.NoError(t, err)

	snowCtx.TeleporterSigner = teleporter.NewSigner(blsSecretKey, snowCtx.ChainID)
	warpBackend := warp.NewWarpBackend(snowCtx, database, 100)

	msg, err := teleporter.NewUnsignedMessage(snowCtx.ChainID, snowCtx.CChainID, []byte("test"))
	require.NoError(t, err)

	messageID := hashing.ComputeHash256Array(msg.Bytes())
	require.NoError(t, warpBackend.AddMessage(context.Background(), msg))
	signature, err := warpBackend.GetSignature(context.Background(), messageID)
	require.NoError(t, err)
	unknownMessageID := ids.GenerateTestID()

	mockHandlerStats := &stats.MockHandlerStats{}
	signatureRequestHandler := NewSignatureRequestHandler(warpBackend, message.Codec, mockHandlerStats)

	tests := map[string]struct {
		setup       func() (request message.SignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *stats.MockHandlerStats)
	}{
		"normal": {
			setup: func() (request message.SignatureRequest, expectedResponse []byte) {
				return message.SignatureRequest{
					MessageID: messageID,
				}, signature[:]
			},
			verifyStats: func(t *testing.T, stats *stats.MockHandlerStats) {
				require.EqualValues(t, 1, mockHandlerStats.SignatureRequestCount)
				require.EqualValues(t, 1, mockHandlerStats.SignatureRequestHit)
				require.EqualValues(t, 0, mockHandlerStats.SignatureRequestMiss)
				require.Greater(t, mockHandlerStats.SignatureRequestDuration, time.Duration(0))
			},
		},
		"unknown": {
			setup: func() (request message.SignatureRequest, expectedResponse []byte) {
				return message.SignatureRequest{
					MessageID: unknownMessageID,
				}, nil
			},
			verifyStats: func(t *testing.T, stats *stats.MockHandlerStats) {
				require.EqualValues(t, 1, mockHandlerStats.SignatureRequestCount)
				require.EqualValues(t, 1, mockHandlerStats.SignatureRequestMiss)
				require.EqualValues(t, 0, mockHandlerStats.SignatureRequestHit)
				require.Greater(t, mockHandlerStats.SignatureRequestDuration, time.Duration(0))
			},
		},
	}

	for name, test := range tests {
		// Reset stats before each test
		mockHandlerStats.Reset()

		t.Run(name, func(t *testing.T) {
			request, expectedResponse := test.setup()
			responseBytes, err := signatureRequestHandler.OnSignatureRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
			require.NoError(t, err)

			// If the expected response is empty, assert that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				test.verifyStats(t, mockHandlerStats)
				require.Len(t, responseBytes, 0, "expected response to be empty")
				return
			}
			var response message.SignatureResponse
			_, err = message.Codec.Unmarshal(responseBytes, &response)
			require.NoError(t, err, "error unmarshalling SignatureResponse")

			require.Equal(t, expectedResponse, response.Signature[:])
			test.verifyStats(t, mockHandlerStats)
		})
	}
}
