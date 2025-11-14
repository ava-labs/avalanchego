// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"
	"slices"
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
	"github.com/ava-labs/avalanchego/vms/evm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	networkID           uint32 = 54321
	sourceChainID              = ids.GenerateTestID()
	testSourceAddress          = utils.RandomBytes(20)
	testPayload                = []byte("test")
	testUnsignedMessage *warp.UnsignedMessage

	// emptyBlockStore returns an error if a block is requested
	emptyBlockStore BlockStore = testBlockStore(func(_ context.Context, _ ids.ID) error {
		return database.ErrNotFound
	})
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

// testBlockStore implements BlockStore for testing
type testBlockStore func(ctx context.Context, blockID ids.ID) error

func (t testBlockStore) GetBlock(ctx context.Context, blockID ids.ID) error {
	return t(ctx, blockID)
}

// makeBlockStore returns a new BlockStore that returns the provided blocks.
// If a block is requested that isn't part of the provided blocks, an error is
// returned.
func makeBlockStore(blkIDs ...ids.ID) BlockStore {
	return testBlockStore(func(_ context.Context, blkID ids.ID) error {
		if !slices.Contains(blkIDs, blkID) {
			return database.ErrNotFound
		}
		return nil
	})
}

func TestAddAndGetValidMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)

	// Add testUnsignedMessage to the warp backend
	require.NoError(t, messageDB.Add(testUnsignedMessage))

	// Verify that a signature is returned successfully, and compare to expected signature.
	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestAddAndGetUnknownMessage(t *testing.T) {
	db := memdb.New()

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil)

	// Create an unknown message with empty source address to test parse failure
	unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
	require.NoError(t, err)
	unknownMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownPayload.Bytes())
	require.NoError(t, err)

	// Try to verify an unknown message - should fail with parse error
	appErr := verifier.Verify(t.Context(), unknownMessage)
	require.ErrorIs(t, appErr, &common.AppError{Code: ParseErrCode})
}

func TestGetBlockSignature(t *testing.T) {
	require := require.New(t)

	blkID := ids.GenerateTestID()
	blockStore := makeBlockStore(blkID)
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, blockStore, nil)

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(err)
	unsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)
	expectedSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(err)

	// Verify the block message
	appErr := verifier.Verify(t.Context(), unsignedMessage)
	require.Nil(appErr)

	// Then sign it
	signature, err := warpSigner.Sign(unsignedMessage)
	require.NoError(err)
	require.Equal(expectedSig, signature)

	// Test that an unknown block fails verification
	unknownBlockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(err)
	unknownUnsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownBlockHashPayload.Bytes())
	require.NoError(err)
	unknownAppErr := verifier.Verify(t.Context(), unknownUnsignedMessage)
	require.ErrorIs(unknownAppErr, &common.AppError{Code: VerifyErrCode})
}

func TestVerifierKnownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil)

	// Add testUnsignedMessage to the database
	require.NoError(t, messageDB.Add(testUnsignedMessage))

	// Known messages in the DB should pass verification
	appErr := verifier.Verify(t.Context(), testUnsignedMessage)
	require.Nil(t, appErr)

	// And can be signed
	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	expectedSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, expectedSig, signature)
}

func TestKnownMessageSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	tests := map[string]struct {
		setup       func(db *DB) (request []byte, expectedResponse []byte)
		verifyStats func(t *testing.T, v *Verifier)
		err         *common.AppError
	}{
		"known message": {
			setup: func(db *DB) (request []byte, expectedResponse []byte) {
				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
				require.NoError(t, err)
				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(msg)
				require.NoError(t, err)
				require.NoError(t, db.Add(msg))
				return msg.Bytes(), signature
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Zero(t, v.messageParseFail.Snapshot().Count())
				require.Zero(t, v.blockValidationFail.Snapshot().Count())
			},
		},
		"unknown message": {
			setup: func(_ *DB) (request []byte, expectedResponse []byte) {
				unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
				require.NoError(t, err)
				unknownMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return unknownMessage.Bytes(), nil
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Equal(t, int64(1), v.messageParseFail.Snapshot().Count())
				require.Zero(t, v.blockValidationFail.Snapshot().Count())
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
				db := NewDB(database)
				v := NewVerifier(db, emptyBlockStore, nil)
				handler := NewHandler(sigCache, v, snowCtx.WarpSigner)

				requestBytes, expectedResponse := test.setup(db)
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)
				test.verifyStats(t, v)

				// If the expected response is empty, assert that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated (handler's cache)
				if withCache {
					require.NotZero(t, sigCache.Len())
				} else {
					require.Zero(t, sigCache.Len())
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
	blockStore := makeBlockStore(knownBlkID)

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
		verifyStats func(t *testing.T, v *Verifier)
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
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Zero(t, v.blockValidationFail.Snapshot().Count())
				require.Zero(t, v.messageParseFail.Snapshot().Count())
			},
		},
		"unknown block": {
			setup: func() (request []byte, expectedResponse []byte) {
				unknownBlockID := ids.GenerateTestID()
				return toMessageBytes(unknownBlockID), nil
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Equal(t, int64(1), v.blockValidationFail.Snapshot().Count())
				require.Zero(t, v.messageParseFail.Snapshot().Count())
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
				db := NewDB(database)
				v := NewVerifier(db, blockStore, nil)
				handler := NewHandler(sigCache, v, snowCtx.WarpSigner)

				requestBytes, expectedResponse := test.setup()
				protoMsg := &sdk.SignatureRequest{Message: requestBytes}
				protoBytes, err := proto.Marshal(protoMsg)
				require.NoError(t, err)
				responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
				require.ErrorIs(t, appErr, test.err)

				test.verifyStats(t, v)

				// If the expected response is empty, require that the handler returns an empty response and return early.
				if len(expectedResponse) == 0 {
					require.Empty(t, responseBytes, "expected response to be empty")
					return
				}
				// check cache is populated (handler's cache)
				if withCache {
					require.NotZero(t, sigCache.Len())
				} else {
					require.Zero(t, sigCache.Len())
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
		uptimePayload, err := message.NewValidatorUptime(vID, 80)
		require.NoError(t, err)
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

		db := NewDB(database)
		verifier := NewVerifier(db, emptyBlockStore, uptimeTracker)
		handler := NewHandler(sigCache, verifier, snowCtx.WarpSigner)

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
