// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import "sync"

// BlockOperationType represents the type of operation to perform on a block.
type BlockOperationType int

const (
	OpAccept BlockOperationType = iota
	OpReject
	OpVerify
)

// String returns the string representation of the block operation.
func (op BlockOperationType) String() string {
	switch op {
	case OpAccept:
		return "accept"
	case OpReject:
		return "reject"
	case OpVerify:
		return "verify"
	default:
		return "unknown"
	}
}

// blockOperation represents a queued block operation.
type blockOperation struct {
	block     EthBlockWrapper
	operation BlockOperationType
}

// blockQueue buffers block operations (accept/reject/verify) that arrive while
// the coordinator is in the Running state. Operations are processed in FIFO order.
// It is cleared (drained) on UpdateSyncTarget to avoid drops and is snapshotted
// at finalization via DequeueBatch. Enqueue is always allowed; a DequeueBatch
// only captures the current buffered operations and clears them, and new enqueues
// after the snapshot are not part of that batch.
type blockQueue struct {
	mu sync.Mutex
	// buffered operations accumulated before finalization
	items []blockOperation
}

// newBlockQueue creates a new empty queue.
func newBlockQueue() *blockQueue {
	return &blockQueue{}
}

// enqueue appends a block operation to the buffer. Returns true if the operation
// was queued, false if the block is nil.
func (q *blockQueue) enqueue(b EthBlockWrapper, op BlockOperationType) bool {
	if b == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, blockOperation{
		block:     b,
		operation: op,
	})
	return true
}

// dequeueBatch returns the current buffered operations and clears the buffer. New
// arrivals after the snapshot are not included and remain buffered for later.
func (q *blockQueue) dequeueBatch() []blockOperation {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

// removeBelowHeight removes all queued blocks with height <= targetHeight.
// This is called after UpdateSyncTarget to remove blocks that will never be executed
// because the sync target has advanced past them.
func (q *blockQueue) removeBelowHeight(targetHeight uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	filtered := q.items[:0]
	for _, op := range q.items {
		ethBlock := op.block.GetEthBlock()
		if ethBlock != nil && ethBlock.NumberU64() > targetHeight {
			filtered = append(filtered, op)
		}
	}
	q.items = filtered
}
