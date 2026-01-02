// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/messages"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/warptest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestAddressedCallSignatures(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := utilstest.NewTestSnowContext(t)

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
				warpBackend, err := NewBackend(
					snowCtx.NetworkID,
					snowCtx.ChainID,
					snowCtx.WarpSigner,
					warptest.EmptyBlockClient,
					nil,
					database,
					sigCache,
					[][]byte{offchainMessage.Bytes()},
				)
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
	snowCtx := utilstest.NewTestSnowContext(t)

	knownBlkID := ids.GenerateTestID()
	blockClient := warptest.MakeBlockClient(knownBlkID)

	toMessageBytes := func(id ids.ID) []byte {
		idPayload, err := payload.NewHash(id)
		require.NoError(t, err)
		msg, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, idPayload.Bytes())
		require.NoError(t, err)
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
					nil,
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

func TestUptimeSignatures(t *testing.T) {
	database := memdb.New()
	snowCtx := utilstest.NewTestSnowContext(t)

	validationID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	startTime := uint64(time.Now().Unix())

	getUptimeMessageBytes := func(sourceAddress []byte, vID ids.ID) ([]byte, *avalancheWarp.UnsignedMessage) {
		uptimePayload, err := messages.NewValidatorUptime(vID, 80)
		require.NoError(t, err)
		addressedCall, err := payload.NewAddressedCall(sourceAddress, uptimePayload.Bytes())
		require.NoError(t, err)
		unsignedMessage, err := avalancheWarp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
		require.NoError(t, err)

		protoMsg := &sdk.SignatureRequest{Message: unsignedMessage.Bytes()}
		protoBytes, err := proto.Marshal(protoMsg)
		require.NoError(t, err)
		return protoBytes, unsignedMessage
	}

	for _, withCache := range []bool{true, false} {
		var sigCache cache.Cacher[ids.ID, []byte]
		if withCache {
			sigCache = lru.NewCache[ids.ID, []byte](100)
		} else {
			sigCache = &cache.Empty[ids.ID, []byte]{}
		}

		// Create a validator state that includes our test validator
		// TODO(JonathanOppenheimer): see func NewTestValidatorState() -- this should be examined
		// when we address the issue of that function.
		validatorState := &validatorstest.State{
			GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
				return map[ids.ID]*validators.GetCurrentValidatorOutput{
					validationID: {
						ValidationID:  validationID,
						NodeID:        nodeID,
						Weight:        1,
						StartTime:     startTime,
						IsActive:      true,
						IsL1Validator: true,
					},
				}, 0, nil
			},
		}

		clk := &mockable.Clock{}
		uptimeTracker, err := uptimetracker.New(
			validatorState,
			snowCtx.SubnetID,
			memdb.New(),
			clk,
		)
		require.NoError(t, err)

		require.NoError(t, uptimeTracker.Sync(t.Context()))

		warpBackend, err := NewBackend(
			snowCtx.NetworkID,
			snowCtx.ChainID,
			snowCtx.WarpSigner,
			warptest.EmptyBlockClient,
			uptimeTracker,
			database,
			sigCache,
			nil,
		)
		require.NoError(t, err)
		handler := acp118.NewCachedHandler(sigCache, warpBackend, snowCtx.WarpSigner)

		// sourceAddress nonZero
		protoBytes, _ := getUptimeMessageBytes([]byte{1, 2, 3}, ids.GenerateTestID())
		_, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
		require.ErrorIs(t, appErr, &common.AppError{Code: VerifyErrCode})
		require.Equal(t, "2: source address should be empty for offchain addressed messages", appErr.Error())

		// not existing validationID
		vID := ids.GenerateTestID()
		protoBytes, _ = getUptimeMessageBytes([]byte{}, vID)
		_, appErr = handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
		require.ErrorIs(t, appErr, &common.AppError{Code: VerifyErrCode})
		require.Equal(t, fmt.Sprintf("2: failed to get uptime: validationID not found: %s", vID), appErr.Error())

		// uptime is less than requested (not connected)
		protoBytes, _ = getUptimeMessageBytes([]byte{}, validationID)
		_, appErr = handler.AppRequest(t.Context(), nodeID, time.Time{}, protoBytes)
		require.ErrorIs(t, appErr, &common.AppError{Code: VerifyErrCode})
		require.Equal(t, fmt.Sprintf("2: current uptime 0 is less than queried uptime 80 for validationID %s", validationID), appErr.Error())

		// uptime is less than requested (not enough time)
		require.NoError(t, uptimeTracker.Connect(nodeID))
		clk.Set(clk.Time().Add(40 * time.Second))
		protoBytes, _ = getUptimeMessageBytes([]byte{}, validationID)
		_, appErr = handler.AppRequest(t.Context(), nodeID, time.Time{}, protoBytes)
		require.ErrorIs(t, appErr, &common.AppError{Code: VerifyErrCode})
		require.Equal(t, fmt.Sprintf("2: current uptime 40 is less than queried uptime 80 for validationID %s", validationID), appErr.Error())

		// valid uptime (enough time has passed)
		clk.Set(clk.Time().Add(40 * time.Second))
		protoBytes, msg := getUptimeMessageBytes([]byte{}, validationID)
		responseBytes, appErr := handler.AppRequest(t.Context(), nodeID, time.Time{}, protoBytes)
		require.Nil(t, appErr)
		expectedSignature, err := snowCtx.WarpSigner.Sign(msg)
		require.NoError(t, err)
		response := &sdk.SignatureResponse{}
		require.NoError(t, proto.Unmarshal(responseBytes, response))
		require.Equal(t, expectedSignature, response.Signature)
	}
}
