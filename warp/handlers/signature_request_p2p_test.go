// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/warp"
	"github.com/ava-labs/subnet-evm/warp/warptest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestMessageSignatureHandlerP2P(t *testing.T) {
	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := bls.NewSecretKey()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)

	addressedPayload, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{1, 2, 3})
	require.NoError(t, err)
	offchainMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedPayload.Bytes())
	require.NoError(t, err)

	backend, err := warp.NewBackend(snowCtx.NetworkID, snowCtx.ChainID, warpSigner, warptest.EmptyBlockClient, database, 100, [][]byte{offchainMessage.Bytes()})
	require.NoError(t, err)

	offchainPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
	require.NoError(t, err)
	msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, offchainPayload.Bytes())
	require.NoError(t, err)
	messageID := msg.ID()
	require.NoError(t, backend.AddMessage(msg))
	signature, err := backend.GetMessageSignature(messageID)
	require.NoError(t, err)
	offchainSignature, err := backend.GetMessageSignature(offchainMessage.ID())
	require.NoError(t, err)

	unknownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("unknown message"))
	require.NoError(t, err)
	unknownMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
	require.NoError(t, err)

	tests := map[string]struct {
		setup       func() (request sdk.SignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *handlerStats)
		err         error
	}{
		"known message": {
			setup: func() (request sdk.SignatureRequest, expectedResponse []byte) {
				return sdk.SignatureRequest{Message: msg.Bytes()}, signature[:]
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
			setup: func() (request sdk.SignatureRequest, expectedResponse []byte) {
				return sdk.SignatureRequest{Message: offchainMessage.Bytes()}, offchainSignature[:]
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
			setup: func() (request sdk.SignatureRequest, expectedResponse []byte) {
				return sdk.SignatureRequest{Message: unknownMessage.Bytes()}, nil
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 1, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 1, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureMiss.Snapshot().Count())
			},
			err: &common.AppError{Code: ErrFailedToGetSig},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewSignatureRequestHandlerP2P(backend, message.Codec)
			handler.stats.Clear()

			request, expectedResponse := test.setup()
			requestBytes, err := proto.Marshal(&request)
			require.NoError(t, err)
			responseBytes, appErr := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, requestBytes)
			if test.err != nil {
				require.ErrorIs(t, appErr, test.err)
			} else {
				require.Nil(t, appErr)
			}

			test.verifyStats(t, handler.stats)

			// If the expected response is empty, assert that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				require.Len(t, responseBytes, 0, "expected response to be empty")
				return
			}
			var response sdk.SignatureResponse
			err = proto.Unmarshal(responseBytes, &response)
			require.NoError(t, err, "error unmarshalling SignatureResponse")

			require.Equal(t, expectedResponse, response.Signature)
		})
	}
}

func TestBlockSignatureHandlerP2P(t *testing.T) {
	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := bls.NewSecretKey()
	require.NoError(t, err)

	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)
	blkID := ids.GenerateTestID()
	blockClient := warptest.MakeBlockClient(blkID)
	backend, err := warp.NewBackend(
		snowCtx.NetworkID,
		snowCtx.ChainID,
		warpSigner,
		blockClient,
		database,
		100,
		nil,
	)
	require.NoError(t, err)

	signature, err := backend.GetBlockSignature(blkID)
	require.NoError(t, err)
	unknownBlockID := ids.GenerateTestID()

	toMessageBytes := func(id ids.ID) []byte {
		idPayload, err := payload.NewHash(id)
		require.NoError(t, err)

		msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, idPayload.Bytes())
		require.NoError(t, err)

		return msg.Bytes()
	}

	tests := map[string]struct {
		setup       func() (request sdk.SignatureRequest, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *handlerStats)
		err         error
	}{
		"known block": {
			setup: func() (request sdk.SignatureRequest, expectedResponse []byte) {
				return sdk.SignatureRequest{Message: toMessageBytes(blkID)}, signature[:]
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
			setup: func() (request sdk.SignatureRequest, expectedResponse []byte) {
				return sdk.SignatureRequest{Message: toMessageBytes(unknownBlockID)}, nil
			},
			verifyStats: func(t *testing.T, stats *handlerStats) {
				require.EqualValues(t, 0, stats.messageSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureHit.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageSignatureMiss.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureRequest.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockSignatureHit.Snapshot().Count())
				require.EqualValues(t, 1, stats.blockSignatureMiss.Snapshot().Count())
			},
			err: &common.AppError{Code: ErrFailedToGetSig},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			handler := NewSignatureRequestHandlerP2P(backend, message.Codec)
			handler.stats.Clear()

			request, expectedResponse := test.setup()
			requestBytes, err := proto.Marshal(&request)
			require.NoError(t, err)
			responseBytes, appErr := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, requestBytes)
			if test.err != nil {
				require.ErrorIs(t, appErr, test.err)
			} else {
				require.Nil(t, appErr)
			}

			test.verifyStats(t, handler.stats)

			// If the expected response is empty, assert that the handler returns an empty response and return early.
			if len(expectedResponse) == 0 {
				require.Len(t, responseBytes, 0, "expected response to be empty")
				return
			}
			var response sdk.SignatureResponse
			err = proto.Unmarshal(responseBytes, &response)
			require.NoError(t, err, "error unmarshalling SignatureResponse")

			require.Equal(t, expectedResponse, response.Signature)
		})
	}
}
