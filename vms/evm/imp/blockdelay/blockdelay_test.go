// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdelay

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestExponent(t *testing.T) {
	set := acp226.DelayExcess(1_000)
	tests := []struct {
		name   string
		excess *acp226.DelayExcess
		want   dynamic.DelayExponent
	}{
		{
			name: "unset",
			want: dynamic.InitialDelayExponent,
		},
		{
			name:   "set",
			excess: &set,
			want:   1_000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Exponent(tt.excess), "Exponent()")
		})
	}
}

// waiterFunc is an [EventWaiter] that calls itself.
type waiterFunc func(context.Context) (snowcommon.Message, error)

func (f waiterFunc) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	return f(ctx)
}

func TestPacerWaitForEvent(t *testing.T) {
	errWait := errors.New("wait failed")
	tests := []struct {
		name string
		// If prevCall is true, an earlier call returning prevErr is made
		// immediately before the one under test.
		prevCall   bool
		prevErr    error
		earliestIn time.Duration
		want       time.Duration
	}{
		{
			name:       "build_time_passed",
			earliestIn: -time.Second,
		},
		{
			name:       "before_build_time",
			earliestIn: 2 * time.Second,
			want:       2 * time.Second,
		},
		{
			name:       "throttled",
			prevCall:   true,
			earliestIn: -time.Second,
			want:       minEventSpacing,
		},
		{
			name:       "build_time_after_throttle",
			prevCall:   true,
			earliestIn: time.Second,
			want:       time.Second,
		},
		{
			name:       "previous_call_failed",
			prevCall:   true,
			prevErr:    errWait,
			earliestIn: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var waitErr error
				p := NewPacer(time.Now, waiterFunc(func(context.Context) (snowcommon.Message, error) {
					return snowcommon.PendingTxs, waitErr
				}))
				if tt.prevCall {
					waitErr = tt.prevErr
					_, err := p.WaitForEvent(t.Context(), time.Time{})
					require.ErrorIs(t, err, tt.prevErr, "WaitForEvent() previous call")
				}

				waitErr = nil
				start := time.Now()
				got, err := p.WaitForEvent(t.Context(), start.Add(tt.earliestIn))
				require.NoError(t, err, "WaitForEvent()")
				require.Equal(t, snowcommon.PendingTxs, got, "WaitForEvent()")
				require.Equal(t, tt.want, time.Since(start), "WaitForEvent() blocking duration")
			})
		})
	}
}

func TestPacerWaitForEventCanceled(t *testing.T) {
	errCause := errors.New("canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errCause)

	p := NewPacer(time.Now, waiterFunc(func(context.Context) (snowcommon.Message, error) {
		t.Error("wrapped EventWaiter called after ctx was canceled")
		return 0, nil
	}))
	_, err := p.WaitForEvent(ctx, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, errCause, "WaitForEvent()")
}
