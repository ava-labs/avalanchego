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

func TestNext(t *testing.T) {
	const parent = dynamic.InitialDelayExponent
	var (
		parentTime = time.Unix(1_000, 0)
		minChild   = parentTime.Add(parent.DelayDuration())
		higher     = parent + 1_000
		lower      = parent - 1_000
		near       = parent + 50
	)
	tests := []struct {
		name      string
		childTime time.Time
		desired   *dynamic.DelayExponent
		want      dynamic.DelayExponent
		wantErr   error
	}{
		{
			name:      "at_minimum",
			childTime: minChild,
			want:      parent,
		},
		{
			name:      "after_minimum",
			childTime: minChild.Add(time.Hour),
			want:      parent,
		},
		{
			name:      "below_minimum",
			childTime: minChild.Add(-time.Millisecond),
			wantErr:   ErrBelowMinimum,
		},
		{
			name:      "toward_higher_desired",
			childTime: minChild,
			desired:   &higher,
			want:      parent + 200, // ACP-226 caps each step at 200
		},
		{
			name:      "toward_lower_desired",
			childTime: minChild,
			desired:   &lower,
			want:      parent - 200,
		},
		{
			name:      "reaches_desired",
			childTime: minChild,
			desired:   &near,
			want:      near,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Next(parentTime, parent, tt.childTime, tt.desired)
			require.ErrorIs(t, err, tt.wantErr, "Next()")
			require.Equal(t, tt.want, got, "Next()")
		})
	}
}

func TestPacerWaitForEvent(t *testing.T) {
	errWait := errors.New("wait failed")
	tests := []struct {
		name string
		// prev, if true, makes a call returning prevErr before the one under
		// test, sincePrev before it.
		prev       bool
		prevErr    error
		sincePrev  time.Duration
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
			prev:       true,
			earliestIn: -time.Second,
			want:       MinEventSpacing,
		},
		{
			name:       "throttle_partly_elapsed",
			prev:       true,
			sincePrev:  60 * time.Millisecond,
			earliestIn: -time.Second,
			want:       MinEventSpacing - 60*time.Millisecond,
		},
		{
			name:       "build_time_after_throttle",
			prev:       true,
			earliestIn: time.Second,
			want:       time.Second,
		},
		{
			name:       "previous_call_failed",
			prev:       true,
			prevErr:    errWait,
			earliestIn: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := NewPacer(time.Now)
				if tt.prev {
					_, err := p.WaitForEvent(t.Context(), time.Time{}, func(context.Context) (snowcommon.Message, error) {
						return snowcommon.PendingTxs, tt.prevErr
					})
					require.ErrorIs(t, err, tt.prevErr, "WaitForEvent() previous call")
					time.Sleep(tt.sincePrev)
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
