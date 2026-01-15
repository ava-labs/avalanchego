// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
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
				ValidationID:       ids.Empty,
				TotalUptimeSeconds: 0,
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
				TotalUptimeSeconds: 12345,
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
				TotalUptimeSeconds: ^uint64(0),
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
			require.Equal(tt.msg.TotalUptimeSeconds, gotMsg.TotalUptimeSeconds)
		})
	}
}

func TestVerifierKnownMessage(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil, prometheus.NewRegistry())

	require.NoError(t, messageDB.Add(testUnsignedMessage))

	appErr := verifier.Verify(t.Context(), testUnsignedMessage)
	require.Nil(t, appErr)

	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	wantSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)

	requireMetrics(t, verifier, metricExpectations{
		messageParseFail:        0,
		addressedCallVerifyFail: 0,
		blockVerifyFail:         0,
		uptimeVerifyFail:        0,
	})
}

func TestVerifierUnknownMessage(t *testing.T) {
	db := memdb.New()

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, nil, nil, prometheus.NewRegistry())

	unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
	require.NoError(t, err)
	unknownMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownPayload.Bytes())
	require.NoError(t, err)

	appErr := verifier.Verify(t.Context(), unknownMessage)
	require.ErrorIs(t, appErr, &common.AppError{Code: ParseErrCode})

	requireMetrics(t, verifier, metricExpectations{
		messageParseFail:        1,
		addressedCallVerifyFail: 0,
		blockVerifyFail:         0,
		uptimeVerifyFail:        0,
	})
}

func TestVerifierBlockMessage(t *testing.T) {
	blkID := ids.GenerateTestID()
	blockStore := testBlockStore(set.Of(blkID))
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)
	verifier := NewVerifier(messageDB, blockStore, nil, prometheus.NewRegistry())

	blockHashPayload, err := payload.NewHash(blkID)
	require.NoError(t, err)
	unsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(t, err)
	wantSig, err := warpSigner.Sign(unsignedMessage)
	require.NoError(t, err)

	appErr := verifier.Verify(t.Context(), unsignedMessage)
	require.Nil(t, appErr)

	signature, err := warpSigner.Sign(unsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)

	requireMetrics(t, verifier, metricExpectations{
		messageParseFail:        0,
		addressedCallVerifyFail: 0,
		blockVerifyFail:         0,
		uptimeVerifyFail:        0,
	})

	unknownBlockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	unknownUnsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, unknownBlockHashPayload.Bytes())
	require.NoError(t, err)
	unknownAppErr := verifier.Verify(t.Context(), unknownUnsignedMessage)
	require.ErrorIs(t, unknownAppErr, &common.AppError{Code: VerifyErrCode})

	requireMetrics(t, verifier, metricExpectations{
		messageParseFail:        0,
		addressedCallVerifyFail: 0,
		blockVerifyFail:         1,
		uptimeVerifyFail:        0,
	})
}

func TestVerifierMalformedPayload(t *testing.T) {
	db := memdb.New()
	messageDB := NewDB(db)
	v := NewVerifier(messageDB, nil, nil, prometheus.NewRegistry())

	invalidPayload := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	msg, err := warp.NewUnsignedMessage(networkID, sourceChainID, invalidPayload)
	require.NoError(t, err)

	appErr := v.Verify(t.Context(), msg)
	require.Equal(t, int32(ParseErrCode), appErr.Code)

	require.Equal(t, float64(1), testutil.ToFloat64(v.messageParseFail))
	require.Equal(t, float64(0), testutil.ToFloat64(v.blockVerifyFail))
}

func TestVerifierMalformedUptimePayload(t *testing.T) {
	db := memdb.New()
	messageDB := NewDB(db)
	v := NewVerifier(messageDB, nil, nil, prometheus.NewRegistry())

	invalidUptimeBytes := []byte{0xFF, 0xFF, 0xFF}
	addressedCall, err := payload.NewAddressedCall([]byte{}, invalidUptimeBytes)
	require.NoError(t, err)

	msg, err := warp.NewUnsignedMessage(networkID, sourceChainID, addressedCall.Bytes())
	require.NoError(t, err)

	appErr := v.Verify(t.Context(), msg)
	require.Equal(t, int32(ParseErrCode), appErr.Code)

	require.Equal(t, float64(1), testutil.ToFloat64(v.messageParseFail))
}

func TestHandlerMessageSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	ctx := t.Context()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	tests := []struct {
		name          string
		setupMessage  func(db *DB) *warp.UnsignedMessage
		wantErrCode   error
		wantSignature bool
		wantMetrics   metricExpectations
	}{
		{
			name: "known message",
			setupMessage: func(db *DB) *warp.UnsignedMessage {
				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
				require.NoError(t, err)
				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
				require.NoError(t, err)
				require.NoError(t, db.Add(msg))
				return msg
			},
			wantSignature: true,
			wantMetrics: metricExpectations{
				messageParseFail: 0,
				blockVerifyFail:  0,
			},
		},
		{
			name: "unknown message",
			setupMessage: func(_ *DB) *warp.UnsignedMessage {
				unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
				require.NoError(t, err)
				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return msg
			},
			wantErrCode: &common.AppError{Code: ParseErrCode},
			wantMetrics: metricExpectations{
				messageParseFail: 1,
				blockVerifyFail:  0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupHandler(t, ctx, memdb.New(), nil, nil, snowCtx.NetworkID, snowCtx.ChainID)

			message := tt.setupMessage(setup.db)
			_, appErr := sendSignatureRequest(t, ctx, setup, message)
			if tt.wantErrCode != nil {
				require.ErrorIs(t, appErr, tt.wantErrCode)
			} else {
				require.Nil(t, appErr)
			}

			requireMetrics(t, setup.verifier, tt.wantMetrics)
		})
	}
}

func TestHandlerBlockSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	ctx := t.Context()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	knownBlkID := ids.GenerateTestID()
	blockStore := testBlockStore(set.Of(knownBlkID))

	tests := []struct {
		name        string
		blockID     ids.ID
		wantErrCode *int32 // nil if no error expected
		wantMetrics metricExpectations
	}{
		{
			name:    "known block",
			blockID: knownBlkID,
			wantMetrics: metricExpectations{
				blockVerifyFail:  0,
				messageParseFail: 0,
			},
		},
		{
			name:        "unknown block",
			blockID:     ids.GenerateTestID(),
			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
			wantMetrics: metricExpectations{
				blockVerifyFail:  1,
				messageParseFail: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupHandler(t, ctx, memdb.New(), blockStore, nil, snowCtx.NetworkID, snowCtx.ChainID)

			hashPayload, err := payload.NewHash(tt.blockID)
			require.NoError(t, err)
			message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
			require.NoError(t, err)

			_, appErr := sendSignatureRequest(t, ctx, setup, message)
			if tt.wantErrCode != nil {
				require.Equal(t, *tt.wantErrCode, appErr.Code)
			} else {
				require.Nil(t, appErr)
			}

			requireMetrics(t, setup.verifier, tt.wantMetrics)
		})
	}
}

func TestHandlerUptimeSignature(t *testing.T) {
	metricstest.WithMetrics(t)

	ctx := t.Context()
	snowCtx := snowtest.Context(t, snowtest.CChainID)

	validationID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	startTime := uint64(time.Now().Unix())
	requestedUptime := uint64(80)

	tests := []struct {
		name               string
		sourceAddress      []byte
		validationID       ids.ID
		setupUptimeTracker func(*uptimetracker.UptimeTracker, *mockable.Clock)
		wantErrCode        *int32 // nil if no error expected
		wantMetrics        metricExpectations
	}{
		{
			name:               "non-empty source address",
			sourceAddress:      []byte{1, 2, 3},
			validationID:       ids.GenerateTestID(),
			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {},
			wantErrCode:        func() *int32 { i := int32(VerifyErrCode); return &i }(),
			wantMetrics: metricExpectations{
				addressedCallVerifyFail: 1,
			},
		},
		{
			name:               "unknown validation ID",
			sourceAddress:      []byte{},
			validationID:       ids.GenerateTestID(),
			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {},
			wantErrCode:        func() *int32 { i := int32(VerifyErrCode); return &i }(),
			wantMetrics: metricExpectations{
				uptimeVerifyFail: 1,
			},
		},
		{
			name:          "validator not connected",
			sourceAddress: []byte{},
			validationID:  validationID,
			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {
			},
			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
			wantMetrics: metricExpectations{
				uptimeVerifyFail: 1,
			},
		},
		{
			name:          "insufficient uptime",
			sourceAddress: []byte{},
			validationID:  validationID,
			setupUptimeTracker: func(tracker *uptimetracker.UptimeTracker, clk *mockable.Clock) {
				require.NoError(t, tracker.Connect(nodeID))
				clk.Set(clk.Time().Add(40 * time.Second))
			},
			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
			wantMetrics: metricExpectations{
				uptimeVerifyFail: 1,
			},
		},
		{
			name:          "sufficient uptime",
			sourceAddress: []byte{},
			validationID:  validationID,
			setupUptimeTracker: func(tracker *uptimetracker.UptimeTracker, clk *mockable.Clock) {
				require.NoError(t, tracker.Connect(nodeID))
				clk.Set(clk.Time().Add(80 * time.Second))
			},
			wantMetrics: metricExpectations{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			uptimeTracker, err := uptimetracker.New(validatorState, snowCtx.SubnetID, memdb.New(), clk)
			require.NoError(t, err)
			require.NoError(t, uptimeTracker.Sync(ctx))

			tt.setupUptimeTracker(uptimeTracker, clk)

			setup := setupHandler(t, ctx, memdb.New(), nil, uptimeTracker, snowCtx.NetworkID, snowCtx.ChainID)

			uptimePayload := &ValidatorUptime{
				ValidationID:       tt.validationID,
				TotalUptimeSeconds: requestedUptime,
			}
			uptimeBytes, err := uptimePayload.Bytes()
			require.NoError(t, err)

			addressedCall, err := payload.NewAddressedCall(tt.sourceAddress, uptimeBytes)
			require.NoError(t, err)

			message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
			require.NoError(t, err)

			_, appErr := sendSignatureRequest(t, ctx, setup, message)
			if tt.wantErrCode != nil {
				require.Equal(t, *tt.wantErrCode, appErr.Code)
			} else {
				require.Nil(t, appErr)
			}

			requireMetrics(t, setup.verifier, tt.wantMetrics)
		})
	}
}

func TestHandlerCacheBehavior(t *testing.T) {
	metricstest.WithMetrics(t)

	ctx := t.Context()
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	setup := setupHandler(t, ctx, memdb.New(), nil, nil, snowCtx.NetworkID, snowCtx.ChainID)

	knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
	require.NoError(t, err)
	message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
	require.NoError(t, err)
	require.NoError(t, setup.db.Add(message))

	firstSignature, appErr := sendSignatureRequest(t, ctx, setup, message)
	require.Nil(t, appErr)

	cachedSig, ok := setup.sigCache.Get(message.ID())
	require.True(t, ok)
	require.Equal(t, firstSignature, cachedSig)

	secondSignature, appErr := sendSignatureRequest(t, ctx, setup, message)
	require.Nil(t, appErr)
	require.Equal(t, firstSignature, secondSignature)
}
