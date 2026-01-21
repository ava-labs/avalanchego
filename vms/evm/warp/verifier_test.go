// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"context"
	"errors"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ VM = (*testVM)(nil)

type testVM struct {
	blks set.Set[ids.ID]
}

func (t *testVM) HasBlock(_ context.Context, blkID ids.ID) error {
	if !t.blks.Contains(blkID) {
		return errors.New("block not found")
	}

	return nil
}

// TODO
// * test signatures from p2p are correct
// * test that signatures from rpc are correct
// * test bad messages
// * test caching
// * we don't fail verification - we fail signing
// 		* looks pretty flimsy

// TestVerifierVerifyBlockHash tests that we are willing to sign payload.Hash
// messages for accepted blocks
func TestVerifierVerifyBlockHash(t *testing.T) {
	tests := []struct {
		name string
		vm   testVM
		hash *warp.UnsignedMessage
		want *common.AppError
	}{
		{
			name: "non-accepted block",
			hash: func() *warp.UnsignedMessage {
				p := &payload.Hash{Hash: ids.ID{1}}
				warp.NewUnsignedMessage()

				return nil
			}(),
			want: &common.AppError{Code: VerifyErrCode},
		},
		{
			name: "accepted block",
			vm:   testVM{blks: set.Of(ids.ID{1})},
			hash: &payload.Hash{Hash: ids.ID{1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewVerifier(
				&NoVerifier{},
				&tt.vm,
				NewDB(memdb.New()),
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)

			got := v.Verify(t.Context(), tt.hash)
			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestBlockVerifierVerifyUnknown tests that all other messages are not signed
func TestVerifierVerifyUnknown(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    *common.AppError
	}{
		{
			// payload.Hash is guaranteed to never be passed to this function but
			// sanity check anyway.
			name: "block hash",
			payload: func() payload.Payload {
				p, err := payload.NewHash(ids.Empty)
				require.NoError(t, err)

				return p
			}(),
			want: &common.AppError{Code: ParseErrCode},
		},
		{
			name: "unknown message",
			payload: func() payload.Payload {
				// payload.Payload is sealed so we cannot implement it - so we use
				// payload.AddressedCall
				p, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{4, 5, 6})
				require.NoError(t, err)

				return p
			}(),
			want: &common.AppError{Code: ParseErrCode},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bv, err := NewBlockVerifier(&testVM{}, prometheus.NewRegistry())
			require.NoError(t, err)

			got := bv.Verify(
				tt.payload,
				prometheus.NewCounter(prometheus.CounterOpts{}),
			)
			require.ErrorIs(t, got, tt.want)
		})
	}
}

// func TestVerifierMalformedPayload(t *testing.T) {
// 	db := memdb.New()
// 	messageDB := NewDB(db)
// 	v, err := NewVerifier(nil, messageDB, prometheus.NewRegistry())
// 	require.NoError(t, err)
//
// 	invalidPayload := []byte{0xFF, 0xFF, 0xFF, 0xFF}
// 	msg, err := warp.NewUnsignedMessage(networkID, sourceChainID, invalidPayload)
// 	require.NoError(t, err)
//
// 	appErr := v.Verify(t.Context(), msg)
// 	require.Equal(t, int32(ParseErrCode), appErr.Code)
//
// 	require.Equal(t, float64(1), testutil.ToFloat64(v.messageParseFail))
// 	// require.Equal(t, float64(0), testutil.ToFloat64(v.blockVerifyFail))
// }

// func TestVerifierMalformedUptimePayload(t *testing.T) {
// 	db := memdb.New()
// 	messageDB := NewDB(db)
// 	v, err := NewVerifier(nil, messageDB, prometheus.NewRegistry())
// 	require.NoError(t, err)
//
// 	invalidUptimeBytes := []byte{0xFF, 0xFF, 0xFF}
// 	addressedCall, err := payload.NewAddressedCall([]byte{}, invalidUptimeBytes)
// 	require.NoError(t, err)
//
// 	msg, err := warp.NewUnsignedMessage(networkID, sourceChainID, addressedCall.Bytes())
// 	require.NoError(t, err)
//
// 	appErr := v.Verify(t.Context(), msg)
// 	require.Equal(t, int32(ParseErrCode), appErr.Code)
//
// 	require.Equal(t, float64(1), testutil.ToFloat64(v.messageParseFail))
// }
//
// func TestHandlerMessageSignature(t *testing.T) {
// 	metricstest.WithMetrics(t)
//
// 	ctx := t.Context()
// 	snowCtx := snowtest.Context(t, snowtest.CChainID)
//
// 	tests := []struct {
// 		name          string
// 		setupMessage  func(db *DB) *warp.UnsignedMessage
// 		wantErrCode   *int32 // nil if no error expected
// 		wantSignature bool
// 		wantMetrics   metricExpectations
// 	}{
// 		{
// 			name: "known message",
// 			setupMessage: func(db *DB) *warp.UnsignedMessage {
// 				knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
// 				require.NoError(t, err)
// 				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
// 				require.NoError(t, err)
// 				require.NoError(t, db.Add(msg))
// 				return msg
// 			},
// 			wantSignature: true,
// 			wantMetrics: metricExpectations{
// 				messageParseFail: 0,
// 				blockVerifyFail:  0,
// 			},
// 		},
// 		{
// 			name: "unknown message",
// 			setupMessage: func(_ *DB) *warp.UnsignedMessage {
// 				unknownPayload, err := payload.NewAddressedCall([]byte{}, []byte("unknown message"))
// 				require.NoError(t, err)
// 				msg, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, unknownPayload.Bytes())
// 				require.NoError(t, err)
// 				return msg
// 			},
// 			wantErrCode: func() *int32 { i := int32(ParseErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				messageParseFail: 1,
// 				blockVerifyFail:  0,
// 			},
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			setup := setupHandler(t, ctx, memdb.New(), nil, nil, snowCtx.NetworkID, snowCtx.ChainID)
//
// 			message := tt.setupMessage(setup.db)
// 			_, appErr := sendSignatureRequest(t, ctx, setup, message)
// 			if tt.wantErrCode != nil {
// 				require.Equal(t, *tt.wantErrCode, appErr.Code)
// 			} else {
// 				require.Nil(t, appErr)
// 			}
//
// 			requireMetrics(t, setup.verifier, tt.wantMetrics)
// 		})
// 	}
// }
//
// func TestHandlerBlockSignature(t *testing.T) {
// 	metricstest.WithMetrics(t)
//
// 	ctx := t.Context()
// 	snowCtx := snowtest.Context(t, snowtest.CChainID)
//
// 	knownBlkID := ids.GenerateTestID()
// 	blockStore := testBlockStore(set.Of(knownBlkID))
//
// 	tests := []struct {
// 		name        string
// 		blockID     ids.ID
// 		wantErrCode *int32 // nil if no error expected
// 		wantMetrics metricExpectations
// 	}{
// 		{
// 			name:    "known block",
// 			blockID: knownBlkID,
// 			wantMetrics: metricExpectations{
// 				blockVerifyFail:  0,
// 				messageParseFail: 0,
// 			},
// 		},
// 		{
// 			name:        "unknown block",
// 			blockID:     ids.GenerateTestID(),
// 			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				blockVerifyFail:  1,
// 				messageParseFail: 0,
// 			},
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			setup := setupHandler(t, ctx, memdb.New(), blockStore, nil, snowCtx.NetworkID, snowCtx.ChainID)
//
// 			hashPayload, err := payload.NewHash(tt.blockID)
// 			require.NoError(t, err)
// 			message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, hashPayload.Bytes())
// 			require.NoError(t, err)
//
// 			_, appErr := sendSignatureRequest(t, ctx, setup, message)
// 			if tt.wantErrCode != nil {
// 				require.Equal(t, *tt.wantErrCode, appErr.Code)
// 			} else {
// 				require.Nil(t, appErr)
// 			}
//
// 			requireMetrics(t, setup.verifier, tt.wantMetrics)
// 		})
// 	}
// }

// func TestHandlerUptimeSignature(t *testing.T) {
// 	metricstest.WithMetrics(t)
//
// 	ctx := t.Context()
// 	snowCtx := snowtest.Context(t, snowtest.CChainID)
//
// 	validationID := ids.GenerateTestID()
// 	nodeID := ids.GenerateTestNodeID()
// 	startTime := uint64(time.Now().Unix())
// 	requestedUptime := uint64(80)
//
// 	tests := []struct {
// 		name               string
// 		sourceAddress      []byte
// 		validationID       ids.ID
// 		setupUptimeTracker func(*uptimetracker.UptimeTracker, *mockable.Clock)
// 		wantErrCode        *int32 // nil if no error expected
// 		wantMetrics        metricExpectations
// 	}{
// 		{
// 			name:               "non-empty source address",
// 			sourceAddress:      []byte{1, 2, 3},
// 			validationID:       ids.GenerateTestID(),
// 			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {},
// 			wantErrCode:        func() *int32 { i := int32(VerifyErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				addressedCallVerifyFail: 1,
// 			},
// 		},
// 		{
// 			name:               "unknown validation ID",
// 			sourceAddress:      []byte{},
// 			validationID:       ids.GenerateTestID(),
// 			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {},
// 			wantErrCode:        func() *int32 { i := int32(VerifyErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				uptimeVerifyFail: 1,
// 			},
// 		},
// 		{
// 			name:          "validator not connected",
// 			sourceAddress: []byte{},
// 			validationID:  validationID,
// 			setupUptimeTracker: func(_ *uptimetracker.UptimeTracker, _ *mockable.Clock) {
// 			},
// 			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				uptimeVerifyFail: 1,
// 			},
// 		},
// 		{
// 			name:          "insufficient uptime",
// 			sourceAddress: []byte{},
// 			validationID:  validationID,
// 			setupUptimeTracker: func(tracker *uptimetracker.UptimeTracker, clk *mockable.Clock) {
// 				require.NoError(t, tracker.Connect(nodeID))
// 				clk.Set(clk.Time().Add(40 * time.Second))
// 			},
// 			wantErrCode: func() *int32 { i := int32(VerifyErrCode); return &i }(),
// 			wantMetrics: metricExpectations{
// 				uptimeVerifyFail: 1,
// 			},
// 		},
// 		{
// 			name:          "sufficient uptime",
// 			sourceAddress: []byte{},
// 			validationID:  validationID,
// 			setupUptimeTracker: func(tracker *uptimetracker.UptimeTracker, clk *mockable.Clock) {
// 				require.NoError(t, tracker.Connect(nodeID))
// 				clk.Set(clk.Time().Add(80 * time.Second))
// 			},
// 			wantMetrics: metricExpectations{},
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			validatorState := &validatorstest.State{
// 				GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
// 					return map[ids.ID]*validators.GetCurrentValidatorOutput{
// 						validationID: {
// 							ValidationID:  validationID,
// 							NodeID:        nodeID,
// 							Weight:        1,
// 							StartTime:     startTime,
// 							IsActive:      true,
// 							IsL1Validator: true,
// 						},
// 					}, 0, nil
// 				},
// 			}
//
// 			clk := &mockable.Clock{}
// 			uptimeTracker, err := uptimetracker.New(validatorState, snowCtx.SubnetID, memdb.New(), clk)
// 			require.NoError(t, err)
// 			require.NoError(t, uptimeTracker.Sync(ctx))
//
// 			tt.setupUptimeTracker(uptimeTracker, clk)
//
// 			setup := setupHandler(t, ctx, memdb.New(), nil, uptimeTracker, snowCtx.NetworkID, snowCtx.ChainID)
//
// 			uptimePayload := &ValidatorUptime{
// 				ValidationID:       tt.validationID,
// 				TotalUptimeSeconds: requestedUptime,
// 			}
// 			uptimeBytes, err := uptimePayload.Bytes()
// 			require.NoError(t, err)
//
// 			addressedCall, err := payload.NewAddressedCall(tt.sourceAddress, uptimeBytes)
// 			require.NoError(t, err)
//
// 			message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
// 			require.NoError(t, err)
//
// 			_, appErr := sendSignatureRequest(t, ctx, setup, message)
// 			if tt.wantErrCode != nil {
// 				require.Equal(t, *tt.wantErrCode, appErr.Code)
// 			} else {
// 				require.Nil(t, appErr)
// 			}
//
// 			requireMetrics(t, setup.verifier, tt.wantMetrics)
// 		})
// 	}
// }

// func TestHandlerCacheBehavior(t *testing.T) {
// 	metricstest.WithMetrics(t)
//
// 	ctx := t.Context()
// 	snowCtx := snowtest.Context(t, snowtest.CChainID)
// 	setup := setupHandler(t, ctx, memdb.New(), nil, nil, snowCtx.NetworkID, snowCtx.ChainID)
//
// 	knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
// 	require.NoError(t, err)
// 	message, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, knownPayload.Bytes())
// 	require.NoError(t, err)
// 	require.NoError(t, setup.db.Add(message))
//
// 	firstSignature, appErr := sendSignatureRequest(t, ctx, setup, message)
// 	require.Nil(t, appErr)
//
// 	cachedSig, ok := setup.sigCache.Get(message.ID())
// 	require.True(t, ok)
// 	require.Equal(t, firstSignature, cachedSig)
//
// 	secondSignature, appErr := sendSignatureRequest(t, ctx, setup, message)
// 	require.Nil(t, appErr)
// 	require.Equal(t, firstSignature, secondSignature)
// }
