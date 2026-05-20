// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"sync"

	"github.com/ava-labs/libevm/common"
)

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
// Blocks below the sync target height are pruned on UpdateSyncTarget and the
// buffer is snapshotted at finalization via dequeueBatch. Enqueue is always
// allowed. A dequeueBatch only captures the current buffered operations and
// clears them. New enqueues after the snapshot are not part of that batch.
type blockQueue struct {
	mu sync.Mutex
	// buffered operations accumulated before finalization
	items []blockOperation

	verifyDedupe verifyDedupeTracker
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

	// Verify may be called multiple times by the engine; dedupe it so we only queue one verify per block.
	var hash common.Hash
	if ethb := b.GetEthBlock(); ethb != nil {
		hash = ethb.Hash()
	}
	if q.verifyDedupe.isQueued(op, hash) {
		// Already queued a verify for this block: still deferred, but don't duplicate.
		return true
	}
	q.verifyDedupe.markQueued(op, hash)

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

// forget clears dedupe markers for the given operations after they have been executed
// (or abandoned due to error). This ensures the verify dedupe map does not grow unbounded.
func (q *blockQueue) forget(ops []blockOperation) {
	if len(ops) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, op := range ops {
		ethb := op.block.GetEthBlock()
		if ethb == nil {
			continue
		}
		q.verifyDedupe.unmarkQueued(op.operation, ethb.Hash())
	}
}

// removeBelowHeight drops blocks with height < targetHeight.
// Called during pivots to discard blocks below the new target.
func (q *blockQueue) removeBelowHeight(targetHeight uint64) {
	q.remove(func(height uint64) bool { return height >= targetHeight })
}

// removeThroughHeight drops blocks with height <= targetHeight.
// Called before batch replay because the commit-target block is already applied by FinalizeVM.
func (q *blockQueue) removeThroughHeight(targetHeight uint64) {
	q.remove(func(height uint64) bool { return height > targetHeight })
}

// remove drops queued blocks for which keep(height) returns false.
func (q *blockQueue) remove(keep func(height uint64) bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	filtered := q.items[:0]
	for _, op := range q.items {
		ethBlock := op.block.GetEthBlock()
		if ethBlock != nil && keep(ethBlock.NumberU64()) {
			filtered = append(filtered, op)
			continue
		}
		if ethBlock != nil {
			q.verifyDedupe.unmarkQueued(op.operation, ethBlock.Hash())
		}
	}
	q.items = filtered
}

// verifyDedupeTracker tracks which blocks already have a queued OpVerify operation.
//
// It is intentionally domain-specific:
// - It only applies to OpVerify (other ops are ignored).
// - enqueue should still report "deferred" on duplicates, but avoid queuing duplicates.
type verifyDedupeTracker struct {
	seen map[common.Hash]struct{}
}

func (d *verifyDedupeTracker) isQueued(op BlockOperationType, h common.Hash) bool {
	if op != OpVerify || d.seen == nil {
		return false
	}
	_, ok := d.seen[h]
	return ok
}

func (d *verifyDedupeTracker) markQueued(op BlockOperationType, h common.Hash) {
	if op != OpVerify {
		return
	}
	if d.seen == nil {
		d.seen = make(map[common.Hash]struct{})
	}
	d.seen[h] = struct{}{}
}

func (d *verifyDedupeTracker) unmarkQueued(op BlockOperationType, h common.Hash) {
	if op != OpVerify || d.seen == nil {
		return
	}
	delete(d.seen, h)
}
