// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lock

import (
	"cmp"
	"context"
	"sync"
)

// ProgressSubscription allows a caller to subscribe to progress updates.
type ProgressSubscription[T cmp.Ordered] struct {
	signal   *Cond
	lock     sync.Mutex
	progress T
}

// NewProgressSubscription returns a new ProgressSubscription with the given initial progress.
func NewProgressSubscription[T cmp.Ordered](initialProgress T) *ProgressSubscription[T] {
	var ps ProgressSubscription[T]
	ps.signal = NewCond(&ps.lock)
	ps.progress = initialProgress
	return &ps
}

// SetProgress updates the progress of this ProgressSubscription to the given value.
// This will unblock any calls to WaitForProgress that are waiting for a progress value above the given value.
func (ps *ProgressSubscription[T]) SetProgress(progress T) {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	ps.progress = progress
	ps.signal.Broadcast()
}

// WaitForProgress blocks until the progress of this ProgressSubscription is above the given value,
// or until the given context is cancelled.
func (ps *ProgressSubscription[T]) WaitForProgress(ctx context.Context, pos T) {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	for pos >= ps.progress {
		if err := ps.signal.Wait(ctx); err != nil {
			return
		}
	}
}
