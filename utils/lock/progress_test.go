// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lock

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestProgressSubscriptionMultipleWaitersUnblockedBySingleSetProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ps := NewProgressSubscription(0)

		const numWaiters = 5

		var eg errgroup.Group

		for i := range numWaiters {
			eg.Go(func() error {
				return ps.WaitForProgress(t.Context(), i)
			})
		}

		time.Sleep(time.Minute)

		synctest.Wait()

		ps.SetProgress(numWaiters)

		require.NoError(t, eg.Wait())
	})
}

func TestProgressSubscriptionNoUpdate(t *testing.T) {
	tests := []struct {
		name      string
		initial   int
		waitUntil int
		want      error
	}{
		{
			name:      "initial > waitUntil",
			initial:   1,
			waitUntil: 0,
			want:      nil,
		},
		{
			name:      "initial == waitUntil",
			initial:   0,
			waitUntil: 0,
			want:      context.Canceled,
		},
		{
			name:      "initial < waitUntil",
			initial:   0,
			waitUntil: 1,
			want:      context.Canceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ps := NewProgressSubscription(test.initial)
			ctx, cancel := context.WithCancel(t.Context())
			go cancel()
			err := ps.WaitForProgress(ctx, test.waitUntil)
			require.ErrorIs(t, err, test.want)
		})
	}
}
