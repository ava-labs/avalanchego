// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

const defaultQueueCapacity = 5000

var (
	errFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")
	errFailedToFinalizeCodeQueue    = errors.New("failed to finalize code queue")
)

// Queue implements the producer side of code fetching.
// It accepts code hashes, persists durable "to-fetch" markers (idempotent per hash),
// and enqueues the hashes as-is onto an internal channel consumed by the code syncer.
// The queue does not perform in-memory deduplication or local-code checks - that is
// the responsibility of the consumer.
type Queue struct {
	db   ethdb.Database
	quit <-chan struct{}

	// `in` and `out` MUST be the same channel. We need to be able to set `in`
	// to nil after closing, to avoid a send-after-close, but
	// [CodeQueue.CodeHashes] MUST NOT return a nil channel otherwise consumers
	// will block permanently.
	in            chan<- common.Hash // Invariant: open or nil, but never closed
	out           <-chan common.Hash // Invariant: never nil
	chanLock      sync.RWMutex
	closeChanOnce sync.Once // See usage in [CodeQueue.closeOutChannelOnce]

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
// The `quit` channel, if non-nil, MUST eventually be closed to avoid leaking a
// goroutine.
func NewQueue(db ethdb.Database, quit <-chan struct{}, opts ...QueueOption) (*Queue, error) {
	// Create with defaults, then apply options.
	q := &Queue{
		db:       db,
		quit:     quit,
		capacity: defaultQueueCapacity,
	}
	options.ApplyTo(q, opts...)

	ch := make(chan common.Hash, q.capacity)
	q.in = ch
	q.out = ch

	if quit != nil {
		// Close the output channel on early shutdown to unblock consumers.
		go func() {
			<-q.quit
			q.closeChannelOnce()
		}()
	}

	// Always initialize eagerly.
	if err := q.init(); err != nil {
		return nil, err
	}
	return q, nil
}

// CodeHashes returns the receive-only channel of code hashes to consume.
func (q *Queue) CodeHashes() <-chan common.Hash {
	return q.out
}

func (q *Queue) closeChannelOnce() bool {
	var done bool
	q.closeChanOnce.Do(func() {
		q.chanLock.Lock()
		defer q.chanLock.Unlock()

		close(q.in)
		// [CodeQueue.AddCode] takes a read lock before accessing `in` and we
		// want it to block instead of allowing a send-after-close. Calling
		// AddCode() after Finalize() isn't valid, and calling it after `quit`
		// is closed will be picked up by the `select` so a nil alternative case
		// is desirable.
		q.in = nil
		done = true
	})
	return done
}

// AddCode persists and enqueues new code hashes.
// Persists idempotent "to-fetch" markers for all inputs and enqueues them as-is.
// Returns errAddCodeAfterFinalize after a clean finalize and errFailedToAddCodeHashesToQueue on early quit.
func (q *Queue) AddCode(codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}

	// Mark this enqueue as in-flight immediately so shutdown paths wait for us
	// before closing the output channel.
	q.chanLock.RLock()
	defer q.chanLock.RUnlock()
	if q.in == nil {
		// Although this will happen anyway once the `select` is reached,
		// bailing early avoids unnecessary database writes.
		return errFailedToAddCodeHashesToQueue
	}

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
		case q.in <- h: // guaranteed to be open or nil, but never closed
		case <-q.quit:
			return errFailedToAddCodeHashesToQueue
		}
	}
	return nil
}

// Finalize signals that no further code hashes will be added.
// Waits for in-flight enqueues to complete, then closes the output channel.
// If the queue was already closed due to early quit, returns errFailedToFinalizeCodeQueue.
func (q *Queue) Finalize() error {
	if !q.closeChannelOnce() {
		return errFailedToFinalizeCodeQueue
	}
	return nil
}

// init enqueues any persisted code markers found on disk.
func (q *Queue) init() error {
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
