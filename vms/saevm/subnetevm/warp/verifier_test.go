// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/messages"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

// stubUptime is a minimal [UptimeSource] mapping validation IDs to uptimes.
// Unknown validation IDs return [errStubUptimeUnknown].
type stubUptime map[ids.ID]time.Duration

var errStubUptimeUnknown = errors.New("stub uptime: unknown validation id")

func (s stubUptime) GetUptime(validationID ids.ID) (time.Duration, time.Time, error) {
	d, ok := s[validationID]
	if !ok {
		return 0, time.Time{}, errStubUptimeUnknown
	}
	return d, time.Time{}, nil
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

// TestUptimeVerifier covers subnet-evm's [saewarp.AddressedCallVerifier]
// extension. The shared verifier's storage/block/dispatch behaviour is pinned
// by the shared package's own tests.
func TestUptimeVerifier(t *testing.T) {
	validationID := ids.GenerateTestID()
	tests := []struct {
		name   string
		uptime UptimeSource
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
			name: "uptime_sufficient",
			uptime: stubUptime{
				validationID: 60 * time.Second,
			},
			call: newUptimeCall(t, validationID, 60),
		},
		{
			name: "uptime_insufficient",
			uptime: stubUptime{
				validationID: 60 * time.Second,
			},
			call: newUptimeCall(t, validationID, 61),
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name:   "unknown_validation_id",
			uptime: stubUptime{},
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

	v := NewVerifier(nil, saewarp.NewStorage(memdb.New()), stubUptime{
		validationID: 60 * time.Second,
	})
	require.Nil(t, v.Verify(t.Context(), m, nil), "Verify() of a sufficient uptime message")
}
