// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

const defaultQueueCapacity = 5000

var (
	_ types.Finalizer = (*Queue)(nil)

	ErrQueueClosed = errors.New("code queue is closed")
)

// Queue implements the producer side of code fetching.
// It accepts code hashes, persists durable "to-fetch" markers (idempotent per hash),
// and asynchronously enqueues the hashes onto an internal channel consumed by the
// code syncer. AddCode never blocks the caller because channel sends are performed by
// background goroutines managed by an internal errgroup.
//
// The queue does not perform in-memory deduplication or local-code checks since that is
// the responsibility of the consumer.
type Queue struct {
	db     ethdb.Database
	hashCh chan common.Hash

	eg     *errgroup.Group
	cancel context.CancelFunc // cancels the errgroup's internal context
	done   <-chan struct{}    // errgroup context's Done channel, used in AddCode select

	closeMu   sync.RWMutex // guards closed and prevents eg.Go after eg.Wait
	closeOnce sync.Once    // guards close(hashCh), called by Finalize or Shutdown
	closed    bool         // prevents eg.Go after eg.Wait

	capacity int
}

type QueueOption = options.Option[Queue]

// WithCapacity overrides the queue buffer capacity.
func WithCapacity(n int) QueueOption {
	return options.Func[Queue](func(q *Queue) {
		if n > 0 {
			q.capacity = n
		}
	})
}

// NewQueue creates a new code queue applying optional functional options.
// Lifecycle is managed internally: call [Finalize] for normal completion
// or [Shutdown] for cancellation. Both are safe to call in any order.
func NewQueue(db ethdb.Database, opts ...QueueOption) (*Queue, error) {
	q := &Queue{
		db:       db,
		capacity: defaultQueueCapacity,
	}
	options.ApplyTo(q, opts...)

	q.hashCh = make(chan common.Hash, q.capacity)

	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	q.eg, ctx = errgroup.WithContext(ctx)
	q.done = ctx.Done()

	if err := q.init(); err != nil {
		cancel()
		return nil, err
	}
	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *Queue) CodeHashes() <-chan common.Hash {
	return q.hashCh
}

// AddCode persists code hashes as durable disk markers and enqueues them
// for the consumer. Never blocks the caller because channel sends happen in a
// background goroutine. Returns [ErrQueueClosed] after [Shutdown] or [Finalize].
func (q *Queue) AddCode(ctx context.Context, codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Shared lock: concurrent AddCode calls are allowed, but stop() blocks
	// until all in-flight AddCode calls release before proceeding to eg.Wait.
	q.closeMu.RLock()
	defer q.closeMu.RUnlock()

	if q.closed {
		return ErrQueueClosed
	}

	// Persist all input hashes as to-fetch markers. Consumer will dedupe and
	// skip already-present code. Markers are keyed by code hash, so repeated
	// persists overwrite the same key. The consumer deletes the marker after
	// fulfilling the request (or when it detects code is already present).
	batch := q.db.NewBatch()
	for _, codeHash := range codeHashes {
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to write code to fetch marker: %w", err)
		}
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}

	// Defensive copy: the goroutine outlives the caller and must not share
	// the backing array.
	hashesCopy := slices.Clone(codeHashes)

	// Spawn a goroutine to push to the channel.
	// The goroutine may block on channel send but does NOT block the caller.
	q.eg.Go(func() error {
		for _, h := range hashesCopy {
			select {
			case q.hashCh <- h:
			case <-q.done:
				return nil // cancelled, hashes remain as disk markers
			case <-ctx.Done():
				return nil // caller context cancelled
			}
		}
		return nil
	})

	return nil
}

// Finalize waits for all sends to complete, then closes the channel.
// Idempotent with [Shutdown].
func (q *Queue) Finalize() error {
	q.stop(false)
	return nil
}

// Shutdown cancels stuck goroutines, waits for exit, then closes the channel.
// Unsent hashes are safe because disk markers are written before goroutines
// are spawned. On restart, [init] recovers them via [recoverUnfetchedCodeHashes].
// Idempotent with [Finalize].
func (q *Queue) Shutdown() {
	q.stop(true)
}

// markClosed prevents new AddCode goroutines. Must precede eg.Wait.
func (q *Queue) markClosed() {
	q.closeMu.Lock()
	defer q.closeMu.Unlock()
	q.closed = true
}

// stop drains in-flight goroutines and closes the channel.
// If shouldCancel is true, stuck goroutines are unblocked first.
func (q *Queue) stop(shouldCancel bool) {
	q.markClosed()
	if shouldCancel {
		q.cancel()
	}
	// The errgroup goroutines spawned by AddCode never return errors,
	// so it is safe to drop the error here.
	_ = q.eg.Wait()
	q.closeOnce.Do(func() {
		close(q.hashCh)
	})
}

// init enqueues any persisted code markers found on disk.
func (q *Queue) init() error {
	// Recover any persisted code markers and enqueue them.
	// Note: dbCodeHashes are already present as "to-fetch" markers. AddCode will
	// re-persist them, which is a trivial redundancy that happens only on resume
	// (e.g., after restart). We accept this to keep the code simple.
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return fmt.Errorf("unable to recover previous sync state: %w", err)
	}
	// Use context.Background() since init() runs during construction before
	// sync starts. The queue is not closed yet, so AddCode will always succeed.
	if err := q.AddCode(context.Background(), dbCodeHashes); err != nil {
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

		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return nil, fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
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
