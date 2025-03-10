// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/coreth/plugin/evm/testutils"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/warp/warptest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestAddressedCallSignatures(t *testing.T) {
	testutils.WithMetrics(t)

	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)

	offChainPayload, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{1, 2, 3})
	require.NoError(t, err)
	offchainMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, offChainPayload.Bytes())
	require.NoError(t, err)
	offchainSignature, err := warpSigner.Sign(offchainMessage)
	require.NoError(t, err)

	tests := map[string]struct {
		setup       func(backend Backend) (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, stats *verifierStats)
		err         error
	}{
		"known message": {
			setup: func(backend Backend) (request []byte, expectedResponse []byte) {
				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
				require.NoError(t, err)
				msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
				require.NoError(t, err)
				signature, err := warpSigner.Sign(msg)
				require.NoError(t, err)

				backend.AddMessage(msg)
				return msg.Bytes(), signature[:]
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.EqualValues(t, 0, stats.messageParseFail.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockValidationFail.Snapshot().Count())
			},
		},
		"offchain message": {
			setup: func(_ Backend) (request []byte, expectedResponse []byte) {
				return offchainMessage.Bytes(), offchainSignature[:]
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.EqualValues(t, 0, stats.messageParseFail.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockValidationFail.Snapshot().Count())
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
				require.EqualValues(t, 1, stats.messageParseFail.Snapshot().Count())
				require.EqualValues(t, 0, stats.blockValidationFail.Snapshot().Count())
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
					sigCache = &cache.LRU[ids.ID, []byte]{Size: 100}
				} else {
					sigCache = &cache.Empty[ids.ID, []byte]{}
				}
				warpBackend, err := NewBackend(snowCtx.NetworkID, snowCtx.ChainID, warpSigner, warptest.EmptyBlockClient, database, sigCache, [][]byte{offchainMessage.Bytes()})
				require.NoError(t, err)
				handler := acp118.NewCachedHandler(sigCache, warpBackend, warpSigner)

				requestBytes, expectedResponse := test.setup(warpBackend)
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				if test.err != nil {
					require.Error(t, appErr)
					require.ErrorIs(t, appErr, test.err)
				} else {
					require.Nil(t, appErr)
				}

				test.verifyStats(t, warpBackend.(*backend).stats)

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Len(t, responseBytes, 0, "expected response to be empty")
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
	testutils.WithMetrics(t)

	database := memdb.New()
	snowCtx := utils.TestSnowContext()
	blsSecretKey, err := localsigner.New()
	require.NoError(t, err)

	warpSigner := avalancheWarp.NewSigner(blsSecretKey, snowCtx.NetworkID, snowCtx.ChainID)
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
		err         error
	}{
		"known block": {
			setup: func() (request []byte, expectedResponse []byte) {
				hashPayload, err := payload.NewHash(knownBlkID)
				require.NoError(t, err)
				unsignedMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
				require.NoError(t, err)
				signature, err := warpSigner.Sign(unsignedMessage)
				require.NoError(t, err)
				return toMessageBytes(knownBlkID), signature[:]
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.EqualValues(t, 0, stats.blockValidationFail.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageParseFail.Snapshot().Count())
			},
		},
		"unknown block": {
			setup: func() (request []byte, expectedResponse []byte) {
				unknownBlockID := ids.GenerateTestID()
				return toMessageBytes(unknownBlockID), nil
			},
			verifyStats: func(t *testing.T, stats *verifierStats) {
				require.EqualValues(t, 1, stats.blockValidationFail.Snapshot().Count())
				require.EqualValues(t, 0, stats.messageParseFail.Snapshot().Count())
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
					sigCache = &cache.LRU[ids.ID, []byte]{Size: 100}
				} else {
					sigCache = &cache.Empty[ids.ID, []byte]{}
				}
				warpBackend, err := NewBackend(
					snowCtx.NetworkID,
					snowCtx.ChainID,
					warpSigner,
					blockClient,
					database,
					sigCache,
					nil,
				)
				require.NoError(t, err)
				handler := acp118.NewCachedHandler(sigCache, warpBackend, warpSigner)

				requestBytes, expectedResponse := test.setup()
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				if test.err != nil {
					require.NotNil(t, appErr)
					require.ErrorIs(t, test.err, appErr)
				} else {
					require.Nil(t, appErr)
				}

				test.verifyStats(t, warpBackend.(*backend).stats)

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Len(t, responseBytes, 0, "expected response to be empty")
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
