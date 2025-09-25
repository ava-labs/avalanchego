// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
)

// closeState models the lifecycle of the output channel.
type closeState uint32

const (
	defaultQueueCapacity = 5000

	closeStateOpen      closeState = 0
	closeStateFinalized closeState = 1
	closeStateQuit      closeState = 2
)

var (
	errFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")
	errFailedToFinalizeCodeQueue    = errors.New("failed to finalize code queue")
)

// CodeQueue implements the producer side of code fetching.
// It accepts code hashes, persists durable "to-fetch" markers (idempotent per hash),
// and enqueues the hashes as-is onto an internal channel consumed by the code syncer.
// The queue does not perform in-memory deduplication or local-code checks - that is
// the responsibility of the consumer.
type CodeQueue struct {
	db   ethdb.Database
	quit <-chan struct{}

	// Closed by [CodeQueue.Finalize] after the WaitGroup unblocks.
	out       chan common.Hash
	enqueueWG sync.WaitGroup

	// Indicates why/if the output channel was closed.
	closed atomicCloseState
}

// TODO: this will be migrated to using libevm's options pattern in a follow-up PR.
type codeQueueOptions struct {
	capacity int
}

type CodeQueueOption func(*codeQueueOptions)

// WithCapacity overrides the queue buffer capacity.
func WithCapacity(n int) CodeQueueOption {
	return func(o *codeQueueOptions) {
		if n > 0 {
			o.capacity = n
		}
	}
}

// NewCodeQueue creates a new code queue applying optional functional options.
func NewCodeQueue(db ethdb.Database, quit <-chan struct{}, opts ...CodeQueueOption) (*CodeQueue, error) {
	// Apply defaults then options.
	o := codeQueueOptions{
		capacity: defaultQueueCapacity,
	}
	for _, opt := range opts {
		opt(&o)
	}

	q := &CodeQueue{
		db:   db,
		out:  make(chan common.Hash, o.capacity),
		quit: quit,
	}

	// Close the output channel on early shutdown to unblock consumers.
	go func() {
		<-q.quit
		// Transition to quit, wait for in-flight enqueues to finish,
		// then close the output channel to signal consumers.
		q.closed.markQuitAndClose(q.out, &q.enqueueWG)
	}()

	// Always initialize eagerly.
	if err := q.init(); err != nil {
		return nil, err
	}

	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *CodeQueue) CodeHashes() <-chan common.Hash {
	return q.out
}

// AddCode persists and enqueues new code hashes.
// Persists idempotent "to-fetch" markers for all inputs and enqueues them as-is.
// Returns errAddCodeAfterFinalize after a clean finalize and errFailedToAddCodeHashesToQueue on early quit.
func (q *CodeQueue) AddCode(codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}

	// If the queue has quit, do not attempt to send to the closed channel.
	if q.closed.didQuit() {
		return errFailedToAddCodeHashesToQueue
	}

	// Mark this enqueue as in-flight immediately so shutdown paths wait for us
	// before closing the output channel.
	q.enqueueWG.Add(1)
	defer q.enqueueWG.Done()

	batch := q.db.NewBatch()
	// Persist all input hashes as to-fetch markers. Consumer will dedupe and skip
	// already-present code. Persisting all enables consumer-side retry.
	// Note: markers are keyed by code hash, so repeated persists overwrite the same
	// key rather than growing DB usage. The consumer deletes the marker after
	// fulfilling the request (or when it detects code is already present).
	for _, codeHash := range codeHashes {
		customrawdb.AddCodeToFetch(batch, codeHash)
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}

	for _, h := range codeHashes {
		select {
		case q.out <- h:
		case <-q.quit:
			return errFailedToAddCodeHashesToQueue
		}
	}
	return nil
}

// Finalize signals that no further code hashes will be added.
// Waits for in-flight enqueues to complete, then closes the output channel.
// If the queue was already closed due to early quit, returns errFailedToFinalizeCodeQueue.
func (q *CodeQueue) Finalize() error {
	// Attempt to transition to finalized. If we win the CAS, wait for in-flight
	// enqueues to complete and close the output channel. If we lose, return an
	// error only if the queue has already quit - otherwise, it was already finalized.
	if q.closed.canTransitionToFinalized() {
		q.enqueueWG.Wait()
		close(q.out)
		return nil
	}

	// If CAS fails, check if the queue has already quit.
	// NOTE: Callers who do not care about the error can ignore it.
	if q.closed.didQuit() {
		return errFailedToFinalizeCodeQueue
	}

	// Already finalized - nothing to do.
	return nil
}

// init enqueues any persisted code markers found on disk.
func (q *CodeQueue) init() error {
	// Recover any persisted code markers and enqueue them.
	// Note: dbCodeHashes are already present as "to-fetch" markers. addCode will
	// re-persist them, which is a trivial redundancy that happens only on resume
	// (e.g., after restart). We accept this to keep the code simple.
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return fmt.Errorf("unable to recover previous sync state: %w", err)
	}
	if err := q.AddCode(dbCodeHashes); err != nil {
		return fmt.Errorf("unable to resume previous sync: %w", err)
	}

	return nil
}

// recoverUnfetchedCodeHashes cleans out any codeToFetch markers from the database that are no longer
// needed and returns any outstanding markers to the queue.
func recoverUnfetchedCodeHashes(db ethdb.Database) ([]common.Hash, error) {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	batch := db.NewBatch()
	var codeHashes []common.Hash

	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])

		// If we already have the codeHash, delete the marker from the database and continue.
		if !rawdb.HasCode(db, codeHash) {
			codeHashes = append(codeHashes, codeHash)
			continue
		}

		customrawdb.DeleteCodeToFetch(batch, codeHash)
		if batch.ValueSize() < ethdb.IdealBatchSize {
			continue
		}

		// Write the batch to disk if it has reached the ideal batch size.
		if err := batch.Write(); err != nil {
			return nil, fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
		batch.Reset()
	}

	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("failed to iterate code entries to fetch: %w", err)
	}

	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return nil, fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
	}

	return codeHashes, nil
}

// atomicCloseState provides a tiny typed wrapper around an atomic uint32.
// It exposes enum-like operations to make intent explicit at call sites.
type atomicCloseState struct {
	v atomic.Uint32
}

func (s *atomicCloseState) get() closeState {
	return closeState(s.v.Load())
}

func (s *atomicCloseState) transition(oldState, newState closeState) bool {
	return s.v.CompareAndSwap(uint32(oldState), uint32(newState))
}

func (s *atomicCloseState) markQuitAndClose(out chan common.Hash, wg *sync.WaitGroup) {
	if s.transition(closeStateOpen, closeStateQuit) {
		// Ensure all in-flight enqueues have returned before closing the channel
		// to avoid racing a send with a close.
		wg.Wait()
		close(out)
	}
}

func (s *atomicCloseState) canTransitionToFinalized() bool {
	return s.transition(closeStateOpen, closeStateFinalized)
}

func (s *atomicCloseState) didQuit() bool {
	return s.get() == closeStateQuit
}
