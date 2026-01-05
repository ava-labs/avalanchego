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

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// TestCodecSerialization ensures the serialization format remains stable.
func TestCodecSerialization(t *testing.T) {
	tests := []struct {
		name      string
		msg       *ValidatorUptime
		wantBytes []byte
	}{
		{
			name: "zero values",
			msg: &ValidatorUptime{
				ValidationID: ids.Empty,
				TotalUptime:  0,
			},
			wantBytes: []byte{
				// Codec version (0)
				0x00, 0x00,
				// ValidationID (32 bytes of zeros)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// TotalUptime (8 bytes, uint64 = 0)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "non-zero values",
			msg: &ValidatorUptime{
				ValidationID: ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
				TotalUptime: 12345,
			},
			wantBytes: []byte{
				// Codec version (0)
				0x00, 0x00,
				// ValidationID (32 bytes)
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				// TotalUptime (8 bytes, uint64 = 12345 in big-endian)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
			},
		},
		{
			name: "max uptime",
			msg: &ValidatorUptime{
				ValidationID: ids.ID{
					0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
					0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
					0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
					0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				},
				TotalUptime: ^uint64(0),
			},
			wantBytes: []byte{
				// Codec version (0)
				0x00, 0x00,
				// ValidationID (32 bytes of 0xff)
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
				// TotalUptime (8 bytes, max uint64 in big-endian)
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			gotBytes, err := tt.msg.Bytes()
			require.NoError(err)
			require.Equal(tt.wantBytes, gotBytes)

			gotMsg, err := ParseValidatorUptime(tt.wantBytes)
			require.NoError(err)
			require.Equal(tt.msg.ValidationID, gotMsg.ValidationID)
			require.Equal(tt.msg.TotalUptime, gotMsg.TotalUptime)
		})
	}
}

func TestVerifierKnownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil)

	require.NoError(t, messageDB.Add(testUnsignedMessage))

	// Known messages in the DB should pass verification
	appErr := verifier.Verify(t.Context(), testUnsignedMessage)
	require.Nil(t, appErr)

	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	wantSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)
}

func TestVerifierUnknownMessage(t *testing.T) {
	db := memdb.New()

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil)

	// Create an unknown message with empty source address to test parse failure
	unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
	require.NoError(t, err)
	unknownMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownPayload.Bytes())
	require.NoError(t, err)

	appErr := verifier.Verify(t.Context(), unknownMessage)
	require.ErrorIs(t, appErr, &common.AppError{Code: ParseErrCode})
}

func TestVerifierBlockMessage(t *testing.T) {
	blkID := ids.GenerateTestID()
	blockStore := testBlockStore(set.Of(blkID))
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, blockStore, nil)

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(t, err)
	unsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(t, err)
	wantSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(t, err)

	// Known block should pass verification
	appErr := verifier.Verify(t.Context(), unsignedMessage)
	require.Nil(t, appErr)

	signature, err := warpSigner.Sign(unsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)

	// Unknown block should fail verification
	unknownBlockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	unknownUnsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownBlockHashPayload.Bytes())
	require.NoError(t, err)
	unknownAppErr := verifier.Verify(t.Context(), unknownUnsignedMessage)
	require.ErrorIs(t, unknownAppErr, &common.AppError{Code: VerifyErrCode})
}

func TestHandlerMessageSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	tests := []struct {
		name        string
		setup       func(db *DB) (request []byte, wantResponse []byte)
		verifyStats func(t *testing.T, v *Verifier)
		err         *common.AppError
	}{
		{
			name: "known message",
			setup: func(db *DB) (request []byte, wantResponse []byte) {
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
				require.Zero(t, v.blockVerifyFail.Snapshot().Count())
			},
		},
		{
			name: "unknown message",
			setup: func(_ *DB) (request []byte, wantResponse []byte) {
				unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
				require.NoError(t, err)
				unknownMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return unknownMessage.Bytes(), nil
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Equal(t, int64(1), v.messageParseFail.Snapshot().Count())
				require.Zero(t, v.blockVerifyFail.Snapshot().Count())
			},
			err: &common.AppError{Code: ParseErrCode},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sigCache := lru.NewCache[ids.ID, []byte](100)
			db := NewDB(database)
			v := NewVerifier(db, emptyBlockStore, nil)
			handler := NewHandler(sigCache, v, snowCtx.WarpSigner)

			requestBytes, wantResponse := tt.setup(db)
			protoMsg := &sdk.SignatureRequest{Message: requestBytes}
			protoBytes, err := proto.Marshal(protoMsg)
			require.NoError(t, err)
			responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
			require.ErrorIs(t, appErr, tt.err)
			tt.verifyStats(t, v)

			if len(wantResponse) == 0 {
				require.Empty(t, responseBytes)
				return
			}

			response := &sdk.SignatureResponse{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))
			require.Equal(t, wantResponse, response.Signature)
		})
	}
}

func TestHandlerBlockSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	knownBlkID := ids.GenerateTestID()
	blockStore := testBlockStore(set.Of(knownBlkID))

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

	tests := []struct {
		name        string
		setup       func() (request []byte, wantResponse []byte)
		verifyStats func(t *testing.T, v *Verifier)
		err         *common.AppError
	}{
		{
			name: "known block",
			setup: func() (request []byte, wantResponse []byte) {
				hashPayload, err := payload.NewHash(knownBlkID)
				require.NoError(t, err)
				unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
				require.NoError(t, err)
				signature, err := snowCtx.WarpSigner.Sign(unsignedMessage)
				require.NoError(t, err)
				return toMessageBytes(knownBlkID), signature
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Zero(t, v.blockVerifyFail.Snapshot().Count())
				require.Zero(t, v.messageParseFail.Snapshot().Count())
			},
		},
		{
			name: "unknown block",
			setup: func() (request []byte, wantResponse []byte) {
				unknownBlockID := ids.GenerateTestID()
				return toMessageBytes(unknownBlockID), nil
			},
			verifyStats: func(t *testing.T, v *Verifier) {
				require.Equal(t, int64(1), v.blockVerifyFail.Snapshot().Count())
				require.Zero(t, v.messageParseFail.Snapshot().Count())
			},
			err: &common.AppError{Code: VerifyErrCode},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sigCache := lru.NewCache[ids.ID, []byte](100)
			db := NewDB(database)
			v := NewVerifier(db, blockStore, nil)
			handler := NewHandler(sigCache, v, snowCtx.WarpSigner)

			requestBytes, wantResponse := tt.setup()
			protoMsg := &sdk.SignatureRequest{Message: requestBytes}
			protoBytes, err := proto.Marshal(protoMsg)
			require.NoError(t, err)
			responseBytes, appErr := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, protoBytes)
			require.ErrorIs(t, appErr, tt.err)

			tt.verifyStats(t, v)

			if len(wantResponse) == 0 {
				require.Empty(t, responseBytes)
				return
			}

			var response sdk.SignatureResponse
			require.NoError(t, proto.Unmarshal(responseBytes, &response))
			require.Equal(t, wantResponse, response.Signature)
		})
	}
}

func TestHandlerUptimeSignature(t *testing.T) {
	database := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	validationID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	startTime := uint64(time.Now().Unix())

	getUptimeMessageBytes := func(sourceAddress []byte, vID ids.ID) ([]byte, *warp.UnsignedMessage) {
		uptimePayload := &ValidatorUptime{ValidationID: vID, TotalUptime: 80}
		uptimeBytes, err := uptimePayload.Bytes()
		require.NoError(t, err)
		addressedCall, err := payload.NewAddressedCall(sourceAddress, uptimeBytes)
		require.NoError(t, err)
		unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
		require.NoError(t, err)

		protoMsg := &sdk.SignatureRequest{Message: unsignedMessage.Bytes()}
		protoBytes, err := proto.Marshal(protoMsg)
		require.NoError(t, err)
		return protoBytes, unsignedMessage
	}

	sigCache := lru.NewCache[ids.ID, []byte](100)

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
	wantSignature, err := snowCtx.WarpSigner.Sign(msg)
	require.NoError(t, err)
	response := &sdk.SignatureResponse{}
	require.NoError(t, proto.Unmarshal(responseBytes, response))
	require.Equal(t, wantSignature, response.Signature)
}
