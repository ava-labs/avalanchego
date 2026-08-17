// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// reservedSlots reports the reservation counter, which nothing else exposes.
func reservedSlots(q *queue) int {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	return q.reserved
}

// occupancy reports what the gate counts against the bound.
func occupancy(q *queue) int {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	return len(q.pending) + q.reserved
}

// signalled reports whether a captured room handle was released.
func signalled(room <-chan struct{}) bool {
	select {
	case <-room:
		return true
	default:
		return false
	}
}

// parked captures the handle a reserve of n would wait on.
func parked(t *testing.T, q *queue, n int) <-chan struct{} {
	t.Helper()
	taken, room := q.tryReserve(n)
	require.False(t, taken, "the gate must refuse %d slots here", n)
	return room
}

// fill occupies the whole bound, so any further reserve waits.
func fill(t *testing.T, q *queue) {
	t.Helper()
	require.NoError(t, q.reserve(t.Context(), q.bound))
	q.enqueue(make([]common.Hash, q.bound), q.bound)
}

func TestQueue_ReserveHoldsTheCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		bound     int
		producers int
		rounds    int
		perCall   int
	}{
		{
			name:      "one_producer",
			bound:     8,
			producers: 1,
			rounds:    2000,
			perCall:   3,
		},
		{
			name:      "many_producers",
			bound:     8,
			producers: 8,
			rounds:    500,
			perCall:   3,
		},
		{
			name:      "bound_of_one",
			bound:     1,
			producers: 4,
			rounds:    500,
			perCall:   1,
		},
		{
			name:      "call_equals_bound",
			bound:     4,
			producers: 4,
			rounds:    500,
			perCall:   4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := newQueue(tt.bound)
			hashes := make([]common.Hash, tt.perCall)

			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for {
					if _, closed := q.take(); closed {
						return
					}
					if err := q.wait(t.Context()); err != nil {
						return
					}
				}
			}()

			var eg errgroup.Group
			for range tt.producers {
				eg.Go(func() error {
					for range tt.rounds {
						if err := q.reserve(t.Context(), tt.perCall); err != nil {
							return err
						}
						assert.LessOrEqual(t, occupancy(q), tt.bound)
						q.enqueue(hashes, tt.perCall)
					}
					return nil
				})
			}
			require.NoError(t, eg.Wait())

			// The drainer's stop condition, and producers are done.
			q.close()
			<-drained

			require.Zero(t, reservedSlots(q))
			remaining, _ := q.take()
			require.Empty(t, remaining)
		})
	}
}

// TestQueue_Reserve covers what reserve returns and what it leaves held.
func TestQueue_Reserve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bound int
		// recovered is enqueued outside the bound, the way recovery does, and
		// full occupies the bound through a normal reserve.
		recovered int
		full      bool
		closed    bool
		cancelled bool

		n        int
		wantErr  error
		wantHeld int
	}{
		{
			// Recovery can leave pending above the bound, so a caller wanting
			// nothing must not wait on room that will not appear.
			name:      "zero_never_waits",
			bound:     1,
			recovered: 5,
		},
		{
			name:     "room_available",
			bound:    4,
			n:        2,
			wantHeld: 2,
		},
		{
			name:      "cancelled_holds_nothing",
			bound:     1,
			full:      true,
			cancelled: true,
			n:         1,
			wantErr:   context.Canceled,
		},
		{
			name:    "closed_holds_nothing",
			bound:   1,
			full:    true,
			closed:  true,
			n:       1,
			wantErr: ErrInputClosed,
		},
		{
			// Waiting on room that can never appear, so it goes through once
			// nothing else is outstanding.
			name:     "oversize_admitted_whole",
			bound:    2,
			n:        5,
			wantHeld: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := newQueue(tt.bound)
			q.enqueue(make([]common.Hash, tt.recovered), 0)
			if tt.full {
				fill(t, q)
			}
			if tt.closed {
				q.close()
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			require.ErrorIs(t, q.reserve(ctx, tt.n), tt.wantErr)
			require.Equal(t, tt.wantHeld, reservedSlots(q))
		})
	}
}

// A close releases the batcher's wait, which is its stop condition.
func TestQueue_ClosedReleasesWait(t *testing.T) {
	t.Parallel()

	q := newQueue(1)
	q.close()
	require.NoError(t, q.wait(t.Context()))
}

// Every way of freeing room wakes every waiter, since a drain can free a whole
// batch of slots at once.
func TestQueue_Wakeups(t *testing.T) {
	t.Parallel()

	// wakeSource is what frees room while producers are parked on it.
	type wakeSource int
	const (
		wakeByDrain wakeSource = iota
		wakeByRelease
		wakeBySurplus
	)

	tests := []struct {
		name string
		wake wakeSource
	}{
		{
			name: "drain",
			wake: wakeByDrain,
		},
		{
			name: "release",
			wake: wakeByRelease,
		},
		{
			name: "enqueue_surplus",
			wake: wakeBySurplus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Only a drain has anything queued to give back, so the other two
			// fill the gate with a reservation alone.
			q := newQueue(1)
			if tt.wake == wakeByDrain {
				fill(t, q)
			} else {
				require.NoError(t, q.reserve(t.Context(), q.bound))
			}

			// A buffered signal would release only whichever read it first.
			roomA := parked(t, q, 1)
			roomB := parked(t, q, 1)

			switch tt.wake {
			case wakeByDrain:
				q.take()
			case wakeByRelease:
				q.release(1)
			case wakeBySurplus:
				// Queues less than it reserved, so the difference frees room.
				q.enqueue(nil, 1)
			}

			require.True(t, signalled(roomA))
			require.True(t, signalled(roomB))
		})
	}
}

// Every sequence must hand back what it took, whatever order it took it in.
func TestQueue_ReservedAccounting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// One round is reserve, then enqueue hashes carrying asReserved slots,
		// then release, then drain.
		reserve    int
		hashes     int
		asReserved int
		release    int
		rounds     int

		wantWoken bool
	}{
		{
			// Recovery holds no reservation, so its enqueue must not touch the
			// counter or it goes negative on every startup.
			name:      "recovery_enqueue_is_accounting_free",
			hashes:    5,
			wantWoken: true,
		},
		{
			name:       "reserve_then_enqueue_balances",
			reserve:    2,
			hashes:     2,
			asReserved: 2,
			rounds:     100,
			wantWoken:  true,
		},
		{
			// A negative counter would raise the bound with every stray release,
			// so both release paths clamp at zero instead.
			name:    "stray_release_clamps",
			release: 1,
			rounds:  2,
		},
		{
			name:       "stray_enqueue_clamps",
			hashes:     1,
			asReserved: 1,
			rounds:     2,
			wantWoken:  true,
		},
		{
			// The path taken when every hash was already stored.
			name: "empty_enqueue_wakes_nobody",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := newQueue(4)
			for range max(tt.rounds, 1) {
				require.NoError(t, q.reserve(t.Context(), tt.reserve))
				q.enqueue(make([]common.Hash, tt.hashes), tt.asReserved)
				q.release(tt.release)
				q.take()
			}

			require.Zero(t, reservedSlots(q), "every reservation must be handed back")
			require.Equal(t, tt.wantWoken, len(q.signal) > 0)
		})
	}
}
