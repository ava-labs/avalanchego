// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp/messages"
)

type blocks struct {
	accepted set.Set[ids.ID]
}

func newBlocks(ids ...ids.ID) blocks {
	return blocks{
		set.Of(ids...),
	}
}

func (b blocks) IsAccepted(_ context.Context, id ids.ID) error {
	if !b.accepted.Contains(id) {
		return database.ErrNotFound
	}
	return nil
}

// stubUptime is a minimal `UptimeSource` for tests. It
// returns `uptime` for any `validationID` it has been told about
// (via `set`), and `errNotFound` otherwise.
type stubUptime struct {
	uptimes map[ids.ID]time.Duration
}

func newStubUptime() *stubUptime {
	return &stubUptime{uptimes: make(map[ids.ID]time.Duration)}
}

func (s *stubUptime) set(validationID ids.ID, d time.Duration) {
	s.uptimes[validationID] = d
}

var errStubUptimeUnknown = errors.New("stub uptime: unknown validation id")

func (s *stubUptime) GetUptime(validationID ids.ID) (time.Duration, time.Time, error) {
	d, ok := s.uptimes[validationID]
	if !ok {
		return 0, time.Time{}, errStubUptimeUnknown
	}
	return d, time.Time{}, nil
}

// newOffchainAddressedCall builds a `*payload.AddressedCall` with an
// EMPTY source address (offchain message), wrapping `inner`. The
// verifier requires source addresses to be empty for addressed calls,
// so the storage_test.go helper (which hard-codes a 20-byte random
// source) is unsuitable for verifier success-path tests.
func newOffchainAddressedCall(tb testing.TB, inner []byte) *warp.UnsignedMessage {
	tb.Helper()
	p, err := payload.NewAddressedCall(nil, inner)
	require.NoError(tb, err)
	m, err := warp.NewUnsignedMessage(networkID, sourceChainID, p.Bytes())
	require.NoError(tb, err)
	return m
}

// newUptimeMessage builds an offchain (empty-source) addressed-call
// warp message carrying a `*messages.ValidatorUptime` for
// `validationID` claiming `totalUptime` seconds.
func newUptimeMessage(tb testing.TB, validationID ids.ID, totalUptime uint64) *warp.UnsignedMessage {
	tb.Helper()
	uptimeMsg, err := messages.NewValidatorUptime(validationID, totalUptime)
	require.NoError(tb, err)
	return newOffchainAddressedCall(tb, uptimeMsg.Bytes())
}

func TestVerifier(t *testing.T) {
	addressedCallMsg := newAddressedCall(t, []byte("test"))
	hashMsg, hash := newHash(t)

	invalidPayloadMsg, err := warp.NewUnsignedMessage(networkID, sourceChainID, nil)
	require.NoError(t, err)

	// `addressedCallMsg` carries a non-empty source address (see
	// `newAddressedCall` in storage_test.go), so it must trip the
	// "source address should be empty" check.
	nonEmptySourceMsg := addressedCallMsg

	// Empty-source addressed call wrapping unknown inner bytes: must
	// reach the inner `messages.Parse` arm and fail with ParseErrCode.
	unknownInnerMsg := newOffchainAddressedCall(t, []byte("not a known message"))

	var (
		knownVID    = ids.GenerateTestID()
		unknownVID  = ids.GenerateTestID()
		uptimeOK    = newUptimeMessage(t, knownVID, 60)
		uptimeShort = newUptimeMessage(t, knownVID, 120)
		uptimeMiss  = newUptimeMessage(t, unknownVID, 1)
	)

	tests := []struct {
		name             string
		acceptedBlocks   []ids.ID
		acceptedMessages []*warp.UnsignedMessage
		uptime           func() *stubUptime
		m                *warp.UnsignedMessage
		want             *common.AppError
	}{
		{
			name: "known_message",
			acceptedMessages: []*warp.UnsignedMessage{
				addressedCallMsg,
			},
			m: addressedCallMsg,
		},
		{
			name: "invalid_payload",
			m:    invalidPayloadMsg,
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name: "addressed_call_non_empty_source_address",
			m:    nonEmptySourceMsg,
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name: "addressed_call_unknown_message_type",
			m:    unknownInnerMsg,
			want: &common.AppError{
				Code: ParseErrCode,
			},
		},
		{
			name: "accepted_block",
			acceptedBlocks: []ids.ID{
				hash.Hash,
			},
			m: hashMsg,
		},
		{
			name: "unaccepted_block",
			m:    hashMsg,
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name: "uptime_ok",
			uptime: func() *stubUptime {
				s := newStubUptime()
				s.set(knownVID, 60*time.Second)
				return s
			},
			m: uptimeOK,
		},
		{
			name: "uptime_insufficient",
			uptime: func() *stubUptime {
				s := newStubUptime()
				s.set(knownVID, 60*time.Second)
				return s
			},
			m: uptimeShort,
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
		{
			name:   "uptime_unknown_validation_id",
			uptime: newStubUptime,
			m:      uptimeMiss,
			want: &common.AppError{
				Code: VerifyErrCode,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Keep `uptime` as a genuinely nil
			// `UptimeSource` interface (NOT a non-nil interface
			// holding a nil `*stubUptime`) when the test does not
			// configure one, so the verifier's `v.uptime == nil`
			// check fires.
			var uptime UptimeSource
			if test.uptime != nil {
				uptime = test.uptime()
			}
			v := NewVerifier(
				newBlocks(test.acceptedBlocks...),
				NewStorage(memdb.New(), test.acceptedMessages...),
				uptime,
			)
			err := v.Verify(t.Context(), test.m, nil)
			require.ErrorIs(t, err, test.want)
		})
	}
}
