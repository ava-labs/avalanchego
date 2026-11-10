// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/messages"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	l1validators "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/validators"
	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
)

// newUptime returns an [l1validators.Uptime] tracking a single validator with
// `validationID` that has been connected for `uptime`.
func newUptime(tb testing.TB, validationID ids.ID, uptime time.Duration) *l1validators.Uptime {
	tb.Helper()

	var (
		nodeID    = ids.GenerateTestNodeID()
		startTime = time.Unix(0, 0)
		clock     = &mockable.Clock{}
	)
	clock.Set(startTime)

	state := &validatorstest.State{
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{
				validationID: {
					ValidationID: validationID,
					NodeID:       nodeID,
					StartTime:    uint64(startTime.Unix()), // #nosec G115 -- known positive test timestamp
					IsActive:     true,
				},
			}, 0, nil
		},
	}
	u, err := l1validators.New(state, ids.GenerateTestID(), memdb.New(), clock, logging.NoLog{})
	require.NoError(tb, err, "validators.New()")
	tb.Cleanup(func() {
		require.NoError(tb, u.Shutdown(), "Uptime.Shutdown()")
	})
	require.NoError(tb, u.Dispatch(), "Uptime.Dispatch()")
	require.NoError(tb, u.Connect(nodeID), "Uptime.Connect()")

	clock.Set(startTime.Add(uptime))
	return u
}

func newAddressedCall(tb testing.TB, sourceAddress, data []byte) *payload.AddressedCall {
	tb.Helper()
	p, err := payload.NewAddressedCall(sourceAddress, data)
	require.NoError(tb, err, "payload.NewAddressedCall()")
	return p
}

// newUptimeCall builds an offchain (empty-source) addressed call carrying a
// `*messages.ValidatorUptime` for validationID claiming totalUptime seconds.
func newUptimeCall(tb testing.TB, validationID ids.ID, totalUptime uint64) *payload.AddressedCall {
	tb.Helper()
	uptimeMsg, err := messages.NewValidatorUptime(validationID, totalUptime)
	require.NoError(tb, err, "messages.NewValidatorUptime()")
	return newAddressedCall(tb, nil, uptimeMsg.Bytes())
}

// TestUptimeVerifier covers the L1 VM's [saewarp.AddressedCallVerifier]
// extension. The shared verifier's storage/block/dispatch behaviour is pinned
// by the shared package's own tests.
func TestUptimeVerifier(t *testing.T) {
	validationID := ids.GenerateTestID()
	tests := []struct {
		name   string
		uptime *l1validators.Uptime
		call   *payload.AddressedCall
		want   *common.AppError
	}{
		{
			name: "non_empty_source_address",
			call: newAddressedCall(t, utils.RandomBytes(20), []byte("test")),
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name: "unknown_message_type",
			call: newAddressedCall(t, nil, []byte("not a known message")),
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name: "no_uptime_source",
			call: newUptimeCall(t, validationID, 60),
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name:   "uptime_sufficient",
			uptime: newUptime(t, validationID, 60*time.Second),
			call:   newUptimeCall(t, validationID, 60),
		},
		{
			name:   "uptime_insufficient",
			uptime: newUptime(t, validationID, 60*time.Second),
			call:   newUptimeCall(t, validationID, 61),
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name:   "unknown_validation_id",
			uptime: newUptime(t, ids.GenerateTestID(), time.Minute),
			call:   newUptimeCall(t, validationID, 1),
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := &UptimeVerifier{uptime: test.uptime}
			err := v.VerifyAddressedCall(test.call)
			require.ErrorIs(t, err, test.want, "VerifyAddressedCall()")
		})
	}
}

// TestNewVerifier checks that [NewVerifier] wires the [UptimeVerifier] into
// the shared verifier's addressed-call extension point.
func TestNewVerifier(t *testing.T) {
	validationID := ids.GenerateTestID()
	call := newUptimeCall(t, validationID, 60)
	m, err := warp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), call.Bytes())
	require.NoError(t, err, "warp.NewUnsignedMessage()")

	v := NewVerifier(nil, saewarp.NewStorage(memdb.New()), newUptime(t, validationID, 60*time.Second))
	require.Nil(t, v.Verify(t.Context(), m, nil), "Verify() of a sufficient uptime message")
}
