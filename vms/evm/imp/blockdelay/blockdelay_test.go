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

func TestVerify(t *testing.T) {
	const parent = dynamic.InitialDelayExponent
	var (
		parentTime = time.Unix(1_000, 0)
		minChild   = parentTime.Add(parent.DelayDuration())
	)
	tests := []struct {
		name      string
		childTime time.Time
		wantErr   error
	}{
		{
			name:      "at_minimum",
			childTime: minChild,
		},
		{
			name:      "after_minimum",
			childTime: minChild.Add(time.Hour),
		},
		{
			name:      "below_minimum",
			childTime: minChild.Add(-time.Millisecond),
			wantErr:   ErrBelowMinimum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(parentTime, parent, tt.childTime)
			require.ErrorIs(t, err, tt.wantErr, "Verify()")
		})
	}
}

func TestPacerWaitForEvent(t *testing.T) {
	errWait := errors.New("wait failed")
	tests := []struct {
		name string
		// prev holds the results of earlier calls, made immediately before the
		// one under test.
		prev       []error
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
			prev:       []error{nil},
			earliestIn: -time.Second,
			want:       MinEventSpacing,
		},
		{
			name:       "build_time_after_throttle",
			prev:       []error{nil},
			earliestIn: time.Second,
			want:       time.Second,
		},
		{
			name:       "previous_call_failed",
			prev:       []error{errWait},
			earliestIn: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := NewPacer(time.Now)
				for _, prevErr := range tt.prev {
					_, err := p.WaitForEvent(t.Context(), time.Time{}, func(context.Context) (snowcommon.Message, error) {
						return snowcommon.PendingTxs, prevErr
					})
					require.ErrorIs(t, err, prevErr, "WaitForEvent() previous call")
				}

				start := time.Now()
				got, err := p.WaitForEvent(t.Context(), start.Add(tt.earliestIn), func(context.Context) (snowcommon.Message, error) {
					return snowcommon.StateSyncDone, nil
				})
				require.NoError(t, err, "WaitForEvent()")
				require.Equal(t, snowcommon.StateSyncDone, got, "WaitForEvent()")
				require.Equal(t, tt.want, time.Since(start), "WaitForEvent() blocking duration")
			})
		})
	}
}

func TestPacerWaitForEventCanceled(t *testing.T) {
	errCause := errors.New("canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errCause)

	p := NewPacer(time.Now)
	_, err := p.WaitForEvent(ctx, time.Now().Add(time.Hour), func(context.Context) (snowcommon.Message, error) {
		t.Error("wait called after ctx was canceled")
		return 0, nil
	})
	require.ErrorIs(t, err, errCause, "WaitForEvent()")
}
