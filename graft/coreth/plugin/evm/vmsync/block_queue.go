// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import "sync"

// blockQueue buffers blocks that arrive while the coordinator is in the Running
// state. It is cleared (drained) on UpdateSyncTarget to avoid drops and is
// snapshotted at finalization via DequeueBatch. Enqueue is always allowed; a
// DequeueBatch only captures the current buffered blocks and clears them, and
// new enqueues after the snapshot are not part of that batch.
type blockQueue struct {
	mu sync.Mutex
	// buffered blocks accumulated before finalization
	items []EthBlockWrapper
}

// newBlockQueue creates a new empty queue.
func newBlockQueue() *blockQueue {
	return &blockQueue{}
}

// Enqueue appends a block to the buffer. Returns true if the block was queued,
// false if the block is nil.
func (q *blockQueue) Enqueue(b EthBlockWrapper) bool {
	if b == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, b)
	return true
}

// DequeueBatch returns the current buffered blocks and clears the buffer. New
// arrivals after the snapshot are not included and remain buffered for later.
func (q *blockQueue) DequeueBatch() []EthBlockWrapper {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}
