// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/testutils"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/warp"
	"github.com/ava-labs/coreth/warp/warptest"
	"github.com/stretchr/testify/require"
)

func TestMessageSignatureHandler(t *testing.T) {
	testutils.WithMetrics(t)

	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)

	addressedPayload, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{1, 2, 3})
	require.NoError(t, err)
	offchainMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedPayload.Bytes())
	require.NoError(t, err)

	messageSignatureCache := &cache.LRU[ids.ID, []byte]{Size: 100}
	backend, err := warp.NewBackend(snowCtx.NetworkID, snowCtx.ChainID, warpSigner, warptest.EmptyBlockClient, database, messageSignatureCache, [][]byte{offchainMessage.Bytes()})
	require.NoError(t, err)

	msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, []byte("test"))
	require.NoError(t, err)
	messageID := msg.ID()
	require.NoError(t, backend.AddMessage(msg))
	signature, err := backend.GetMessageSignature(context.TODO(), msg)
	require.NoError(t, err)
	offchainSignature, err := backend.GetMessageSignature(context.TODO(), offchainMessage)
	require.NoError(t, err)

	unknownMessageID := ids.GenerateTestID()

	emptySignature := [bls.SignatureLen]byte{}

	tests := map[string]struct {
		setup       func() (request message.MessageSignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *handlerStats)
	}{
		"known message": {
			setup: func() (request message.MessageSignatureRequest, expectedResponse []byte) {
				return message.MessageSignatureRequest{
					MessageID: messageID,
				}, signature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 1, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureMiss.Snapshot().Count())
			},
		},
		"offchain message": {
			setup: func() (request message.MessageSignatureRequest, expectedResponse []byte) {
				return message.MessageSignatureRequest{
					MessageID: offchainMessage.ID(),
				}, offchainSignature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 1, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureMiss.Snapshot().Count())
			},
		},
		"unknown message": {
			setup: func() (request message.MessageSignatureRequest, expectedResponse []byte) {
				return message.MessageSignatureRequest{
					MessageID: unknownMessageID,
				}, emptySignature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 1, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureMiss.Snapshot().Count())
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewSignatureRequestHandler(backend, message.Codec)

			request, expectedResponse := test.setup()
			responseBytes, err := handler.OnMessageSignatureRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
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

func TestBlockSignatureHandler(t *testing.T) {
	testutils.WithMetrics(t)

	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := localsigner.New()
	require.NoError(t, err)

	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)
	blkID := ids.GenerateTestID()
	blockClient := warptest.MakeBlockClient(blkID)
	messageSignatureCache := &cache.LRU[ids.ID, []byte]{Size: 100}
	backend, err := warp.NewBackend(
		snowCtx.NetworkID,
		snowCtx.ChainID,
		warpSigner,
		blockClient,
		database,
		messageSignatureCache,
		nil,
	)
	require.NoError(t, err)

	signature, err := backend.GetBlockSignature(context.TODO(), blkID)
	require.NoError(t, err)
	unknownMessageID := ids.GenerateTestID()

	emptySignature := [bls.SignatureLen]byte{}

	tests := map[string]struct {
		setup       func() (request message.BlockSignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *handlerStats)
	}{
		"known block": {
			setup: func() (request message.BlockSignatureRequest, expectedResponse []byte) {
				return message.BlockSignatureRequest{
					BlockID: blkID,
				}, signature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 0, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureMiss.Snapshot().Count())
			},
		},
		"unknown block": {
			setup: func() (request message.BlockSignatureRequest, expectedResponse []byte) {
				return message.BlockSignatureRequest{
					BlockID: unknownMessageID,
				}, emptySignature[:]
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 0, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureMiss.Snapshot().Count())
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewSignatureRequestHandler(backend, message.Codec)

			request, expectedResponse := test.setup()
			responseBytes, err := handler.OnBlockSignatureRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
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
