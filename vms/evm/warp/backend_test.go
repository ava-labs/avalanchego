// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/evm/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	networkID           uint32 = 54321
	sourceChainID              = ids.GenerateTestID()
	testSourceAddress          = utils.RandomBytes(20)
	testPayload                = []byte("test")
	testUnsignedMessage *warp.UnsignedMessage
)

func init() {
	testAddressedCallPayload, err := payload.NewAddressedCall(testSourceAddress, testPayload)
	if err != nil {
		panic(err)
	}
	testUnsignedMessage, err = warp.NewUnsignedMessage(networkID, sourceChainID, testAddressedCallPayload.Bytes())
	if err != nil {
		panic(err)
	}
}

func TestAddAndGetValidMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	backend, signer, _, err := NewBackend(networkID, sourceChainID, warpSigner, nil, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)
	ctx := context.Background()

	// Add testUnsignedMessage to the warp backend
	require.NoError(t, backend.AddMessage(ctx, testUnsignedMessage))

	// Verify that a signature is returned successfully, and compare to expected signature.
	signature, err := signer.Sign(ctx, testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestAddAndGetUnknownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	_, signer, _, err := NewBackend(networkID, sourceChainID, warpSigner, nil, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)

	_, err = signer.Sign(context.Background(), testUnsignedMessage)
	require.ErrorIs(t, err, ErrVerifyWarpMessage)
}

func TestGetBlockSignature(t *testing.T) {
	require := require.New(t)

	blkID := ids.GenerateTestID()
	blockStore := warptest.MakeBlockStore(blkID)
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)
	messageSignatureCache := lru.NewCache[ids.ID, []byte](500)
	_, signer, _, err := NewBackend(networkID, sourceChainID, warpSigner, blockStore, nil, db, messageSignatureCache, nil)
	require.NoError(err)
	ctx := context.Background()

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(err)
	unsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)
	expectedSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(err)

	// Callers construct the block message and use Signer.Sign
	signature, err := signer.Sign(ctx, unsignedMessage)
	require.NoError(err)
	require.Equal(expectedSig, signature)

	// Test that an unknown block can still be signed by Signer (verification is caller's responsibility)
	unknownBlockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(err)
	unknownUnsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownBlockHashPayload.Bytes())
	require.NoError(err)
	_, err = signer.Sign(ctx, unknownUnsignedMessage)
	require.ErrorIs(err, ErrVerifyWarpMessage)
}

func TestZeroSizedCache(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	// Verify zero sized cache works normally, because the lru cache will be initialized to size 1 for any size parameter <= 0.
	messageSignatureCache := lru.NewCache[ids.ID, []byte](0)
	backend, signer, _, err := NewBackend(networkID, sourceChainID, warpSigner, nil, nil, db, messageSignatureCache, nil)
	require.NoError(t, err)
	ctx := context.Background()

	// Add testUnsignedMessage to the warp backend
	require.NoError(t, backend.AddMessage(ctx, testUnsignedMessage))

	// Verify that a signature is returned successfully, and compare to expected signature.
	signature, err := signer.Sign(ctx, testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestOffChainMessages(t *testing.T) {
	type test struct {
		offchainMessages [][]byte
		check            func(require *require.Assertions, b Backend, s *Signer)
		err              error
	}
	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	for name, test := range map[string]test{
		"no offchain messages": {},
		"single off-chain message": {
			offchainMessages: [][]byte{
				testUnsignedMessage.Bytes(),
			},
			check: func(require *require.Assertions, b Backend, s *Signer) {
				msg, err := b.GetMessage(testUnsignedMessage.ID())
				require.NoError(err)
				require.Equal(testUnsignedMessage.Bytes(), msg.Bytes())

				signature, err := s.Sign(context.Background(), testUnsignedMessage)
				require.NoError(err)
				expectedSignatureBytes, err := warpSigner.Sign(msg)
				require.NoError(err)
				require.Equal(expectedSignatureBytes, signature)
			},
		},
		"unknown message": {
			check: func(require *require.Assertions, b Backend, s *Signer) {
				_, err := b.GetMessage(testUnsignedMessage.ID())
				require.ErrorIs(err, database.ErrNotFound)

				_, err = s.Sign(context.Background(), testUnsignedMessage)
				require.ErrorIs(err, ErrVerifyWarpMessage)
			},
		},
		"invalid message": {
			offchainMessages: [][]byte{{1, 2, 3}},
			err:              errParsingOffChainMessage,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()

			messageSignatureCache := lru.NewCache[ids.ID, []byte](0)
			backend, signer, _, err := NewBackend(networkID, sourceChainID, warpSigner, nil, nil, db, messageSignatureCache, test.offchainMessages)
			require.ErrorIs(err, test.err)
			if test.check != nil {
				test.check(require, backend, signer)
			}
		})
	}
}

func TestAddressedCallSignatures(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	offChainPayload, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{1, 2, 3})
	require.NoError(t, err)
	offchainMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, offChainPayload.Bytes())
	require.NoError(t, err)
	offchainSignature, err := snowCtx.WarpSigner.Sign(offchainMessage)
	require.NoError(t, err)

	tests := map[string]struct {
		setup       func(backend Backend) (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, b *backend)
		err         *common.AppError
	}{
		"known message": {
			setup: func(backend Backend) (request []byte, expectedResponse []byte) {
				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
				require.NoError(t, err)
				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(msg)
				require.NoError(t, err)
				require.NoError(t, backend.AddMessage(context.Background(), msg))
				return msg.Bytes(), signature
			},
			verifyStats: func(t *testing.T, b *backend) {
				require.Zero(t, b.messageParseFail.Snapshot().Count())
				require.Zero(t, b.blockValidationFail.Snapshot().Count())
			},
		},
		"offchain message": {
			setup: func(_ Backend) (request []byte, expectedResponse []byte) {
				return offchainMessage.Bytes(), offchainSignature
			},
			verifyStats: func(t *testing.T, b *backend) {
				require.Zero(t, b.messageParseFail.Snapshot().Count())
				require.Zero(t, b.blockValidationFail.Snapshot().Count())
			},
		},
		"unknown message": {
			setup: func(_ Backend) (request []byte, expectedResponse []byte) {
				unknownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("unknown message"))
				require.NoError(t, err)
				unknownMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return unknownMessage.Bytes(), nil
			},
			verifyStats: func(t *testing.T, b *backend) {
				require.Equal(t, int64(1), b.messageParseFail.Snapshot().Count())
				require.Zero(t, b.blockValidationFail.Snapshot().Count())
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
				warpBackend, signer, handler, err := NewBackend(snowCtx.NetworkID, snowCtx.ChainID, snowCtx.WarpSigner, warptest.EmptyBlockStore, nil, database, sigCache, [][]byte{offchainMessage.Bytes()})
				require.NoError(t, err)

				requestBytes, expectedResponse := test.setup(warpBackend)
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)
				test.verifyStats(t, warpBackend.(*backend))

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated
				if withCache {
					require.NotZero(t, signer.signatureCache.Len())
				} else {
					require.Zero(t, signer.signatureCache.Len())
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
	blockStore := warptest.MakeBlockStore(knownBlkID)

	toMessageBytes := func(id ids.ID) []byte {
		idPayload, err := payload.NewHash(id)
		if err != nil {
			panic(err)
		}

		msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, idPayload.Bytes())
		if err != nil {
			panic(err)
		}

		return msg.Bytes()
	}

	tests := map[string]struct {
		setup       func() (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, b *backend)
		err         *common.AppError
	}{
		"known block": {
			setup: func() (request []byte, expectedResponse []byte) {
				hashPayload, err := payload.NewHash(knownBlkID)
				require.NoError(t, err)
				unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(unsignedMessage)
				require.NoError(t, err)
				return toMessageBytes(knownBlkID), signature
			},
			verifyStats: func(t *testing.T, b *backend) {
				require.Zero(t, b.blockValidationFail.Snapshot().Count())
				require.Zero(t, b.messageParseFail.Snapshot().Count())
			},
		},
		"unknown block": {
			setup: func() (request []byte, expectedResponse []byte) {
				unknownBlockID := ids.GenerateTestID()
				return toMessageBytes(unknownBlockID), nil
			},
			verifyStats: func(t *testing.T, b *backend) {
				require.Equal(t, int64(1), b.blockValidationFail.Snapshot().Count())
				require.Zero(t, b.messageParseFail.Snapshot().Count())
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
				warpBackend, signer, handler, err := NewBackend(
					snowCtx.NetworkID,
					snowCtx.ChainID,
					snowCtx.WarpSigner,
					blockStore,
					nil,
					database,
					sigCache,
					nil,
				)
				require.NoError(t, err)

				requestBytes, expectedResponse := test.setup()
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)

				test.verifyStats(t, warpBackend.(*backend))

				// If the expected response is empty, require that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated
				if withCache {
					require.NotZero(t, signer.signatureCache.Len())
				} else {
					require.Zero(t, signer.signatureCache.Len())
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
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	validationID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	startTime := uint64(time.Now().Unix())

	getUptimeMessageBytes := func(sourceAddress []byte, vID ids.ID) ([]byte, *warp.UnsignedMessage) {
		uptimePayload := &message.ValidatorUptime{
			ValidationID: vID,
			TotalUptime:  80,
		}
		require.NoError(t, message.Initialize(uptimePayload))
		addressedCall, err := payload.NewAddressedCall(sourceAddress, uptimePayload.Bytes())
		require.NoError(t, err)
		unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
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

		_, _, handler, err := NewBackend(
			snowCtx.NetworkID,
			snowCtx.ChainID,
			snowCtx.WarpSigner,
			warptest.EmptyBlockStore,
			uptimeTracker,
			database,
			sigCache,
			nil,
		)
		require.NoError(t, err)

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
