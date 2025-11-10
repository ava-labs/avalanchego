// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/warp/warptest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestAddressedCallSignatures(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	offChainPayload, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{1, 2, 3})
	require.NoError(t, err)
	offchainMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, offChainPayload.Bytes())
	require.NoError(t, err)
	offchainSignature, err := snowCtx.WarpSigner.Sign(offchainMessage)
	require.NoError(t, err)

	tests := map[string]struct {
		setup       func(backend Backend) (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *verifierStats)
		err         *common.AppError
	}{
		"known message": {
			setup: func(backend Backend) (request []byte, expectedResponse []byte) {
				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
				require.NoError(t, err)
				msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(msg)
				require.NoError(t, err)
				require.NoError(t, backend.AddMessage(msg))
				return msg.Bytes(), signature
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.Zero(t, stats.messageParseFail.Snapshot().Count())
				require.Zero(t, stats.blockValidationFail.Snapshot().Count())
			},
		},
		"offchain message": {
			setup: func(_ Backend) (request []byte, expectedResponse []byte) {
				return offchainMessage.Bytes(), offchainSignature
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.Zero(t, stats.messageParseFail.Snapshot().Count())
				require.Zero(t, stats.blockValidationFail.Snapshot().Count())
			},
		},
		"unknown message": {
			setup: func(_ Backend) (request []byte, expectedResponse []byte) {
				unknownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("unknown message"))
				require.NoError(t, err)
				unknownMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return unknownMessage.Bytes(), nil
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.Equal(t, int64(1), stats.messageParseFail.Snapshot().Count())
				require.Zero(t, stats.blockValidationFail.Snapshot().Count())
			},
			err: &common.AppError{Code: ParseErrCode},
		},
	}

	for name, test := range tests {
		for _, withCache := range []bool{true, false} {
			if withCache {
				name += "_with_cache"
			} else {
				name += "_no_cache"
			}
			t.Run(name, func(t *testing.T) {
				var sigCache cache.Cacher[ids.ID, []byte]
				if withCache {
					sigCache = lru.NewCache[ids.ID, []byte](100)
				} else {
					sigCache = &cache.Empty[ids.ID, []byte]{}
				}
				warpBackend, err := NewBackend(snowCtx.NetworkID, snowCtx.ChainID, snowCtx.WarpSigner, warptest.EmptyBlockClient, database, sigCache, [][]byte{offchainMessage.Bytes()})
				require.NoError(t, err)
				handler := acp118.NewCachedHandler(sigCache, warpBackend, snowCtx.WarpSigner)

				requestBytes, expectedResponse := test.setup(warpBackend)
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)
				test.verifyStats(t, warpBackend.(*backend).stats)

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated
				if withCache {
					require.NotZero(t, warpBackend.(*backend).signatureCache.Len())
				} else {
					require.Zero(t, warpBackend.(*backend).signatureCache.Len())
				}
				response := &sdk.SignatureResponse{}
				require.NoError(t, proto.Unmarshal(responseBytes, response))
				require.NoError(t, err, "error unmarshalling SignatureResponse")

				require.Equal(t, expectedResponse, response.Signature)
			})
		}
	}
}

func TestBlockSignatures(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	knownBlkID := ids.GenerateTestID()
	blockClient := warptest.MakeBlockClient(knownBlkID)

	toMessageBytes := func(id ids.ID) []byte {
		idPayload, err := payload.NewHash(id)
		if err != nil {
			panic(err)
		}

		msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, idPayload.Bytes())
		if err != nil {
			panic(err)
		}

		return msg.Bytes()
	}

	tests := map[string]struct {
		setup       func() (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *verifierStats)
		err         *common.AppError
	}{
		"known block": {
			setup: func() (request []byte, expectedResponse []byte) {
				hashPayload, err := payload.NewHash(knownBlkID)
				require.NoError(t, err)
				unsignedMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(unsignedMessage)
				require.NoError(t, err)
				return toMessageBytes(knownBlkID), signature
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.Zero(t, stats.blockValidationFail.Snapshot().Count())
				require.Zero(t, stats.messageParseFail.Snapshot().Count())
			},
		},
		"unknown block": {
			setup: func() (request []byte, expectedResponse []byte) {
				unknownBlockID := ids.GenerateTestID()
				return toMessageBytes(unknownBlockID), nil
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.Equal(t, int64(1), stats.blockValidationFail.Snapshot().Count())
				require.Zero(t, stats.messageParseFail.Snapshot().Count())
			},
			err: &common.AppError{Code: VerifyErrCode},
		},
	}

	for name, test := range tests {
		for _, withCache := range []bool{true, false} {
			if withCache {
				name += "_with_cache"
			} else {
				name += "_no_cache"
			}
			t.Run(name, func(t *testing.T) {
				var sigCache cache.Cacher[ids.ID, []byte]
				if withCache {
					sigCache = lru.NewCache[ids.ID, []byte](100)
				} else {
					sigCache = &cache.Empty[ids.ID, []byte]{}
				}
				warpBackend, err := NewBackend(
					snowCtx.NetworkID,
					snowCtx.ChainID,
					snowCtx.WarpSigner,
					blockClient,
					database,
					sigCache,
					nil,
				)
				require.NoError(t, err)
				handler := acp118.NewCachedHandler(sigCache, warpBackend, snowCtx.WarpSigner)

				requestBytes, expectedResponse := test.setup()
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)

				test.verifyStats(t, warpBackend.(*backend).stats)

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated
				if withCache {
					require.NotZero(t, warpBackend.(*backend).signatureCache.Len())
				} else {
					require.Zero(t, warpBackend.(*backend).signatureCache.Len())
				}
				var response sdk.SignatureResponse
				err = proto.Unmarshal(responseBytes, &response)
				require.NoError(t, err, "error unmarshalling SignatureResponse")
				require.Equal(t, expectedResponse, response.Signature)
			})
		}
	}
}
