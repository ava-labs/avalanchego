// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

const defaultQueueCapacity = 5000

var (
	_ types.Finalizer = (*Queue)(nil)

	ErrQueueClosed = errors.New("code queue is closed")
)

// Queue is a fan-in/fan-out bridge between code hash producers (leaf sync workers)
// and the code syncer consumer. Producers call [Queue.AddCode] which persists durable
// disk markers and appends hashes to an internal queue. A single background goroutine
// forwards them to the output channel. [Queue.AddCode] never blocks the caller.
//
// Deduplication and local-code checks are the consumer's responsibility.
type Queue struct {
	db  ethdb.Database
	out chan common.Hash // output to consumer

	cancel      context.CancelFunc
	done        <-chan struct{} // cancelled on Shutdown
	forwardDone chan struct{}   // closed when forward() exits

	closeMu     sync.RWMutex
	closeInOnce sync.Once
	closed      bool

	pendingMu sync.Mutex
	pending   []common.Hash
	in        chan struct{} // producer signal, buffered to 1

	capacity int
}

type QueueOption = options.Option[Queue]

func WithCapacity(n int) QueueOption {
	return options.Func[Queue](func(q *Queue) {
		if n > 0 {
			q.capacity = n
		}
	})
}

// NewQueue creates a code queue. Call [Queue.Finalize] for normal completion
// or [Queue.Shutdown] for cancellation. Both are safe to call in any order.
func NewQueue(db ethdb.Database, opts ...QueueOption) (*Queue, error) {
	q := &Queue{
		db:       db,
		capacity: defaultQueueCapacity,
	}
	options.ApplyTo(q, opts...)

	q.out = make(chan common.Hash, q.capacity)
	q.in = make(chan struct{}, 1)
	q.forwardDone = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	q.done = ctx.Done()

	go q.forward()

	if err := q.init(); err != nil {
		cancel()
		<-q.forwardDone
		return nil, err
	}
	return q, nil
}

// CodeHashes returns the receive-only channel consumed by the code syncer.
func (q *Queue) CodeHashes() <-chan common.Hash {
	return q.out
}

// AddCode persists code hashes as durable disk markers and enqueues them
// for the forwarder goroutine. Never blocks the caller.
// Returns [ErrQueueClosed] after [Queue.Shutdown] or [Queue.Finalize].
func (q *Queue) AddCode(ctx context.Context, codeHashes []common.Hash) error {
	if len(codeHashes) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	q.closeMu.RLock()
	defer q.closeMu.RUnlock()

	if q.closed {
		return ErrQueueClosed
	}

	// Persist to-fetch markers keyed by code hash (idempotent overwrites).
	// The consumer deletes markers after fetching or if code is already present.
	batch := q.db.NewBatch()
	for _, codeHash := range codeHashes {
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to write code to fetch marker: %w", err)
		}
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}

	q.pendingMu.Lock()
	q.pending = append(q.pending, codeHashes...)
	q.pendingMu.Unlock()

	// Signal coalescing: skip if the forwarder is already notified.
	select {
	case q.in <- struct{}{}:
	default:
	}

	return nil
}

// Finalize waits for all pending hashes to be sent, then closes out.
// Blocks if no consumer is draining [Queue.CodeHashes]. Idempotent with [Queue.Shutdown].
func (q *Queue) Finalize() error {
	q.stop(false)
	return nil
}

// Shutdown cancels the forwarder, waits for exit, then closes out.
// Unsent hashes are safe as disk markers and will be recovered on restart.
// Idempotent with [Queue.Finalize].
func (q *Queue) Shutdown() {
	q.stop(true)
}

func (q *Queue) markClosed() {
	q.closeMu.Lock()
	defer q.closeMu.Unlock()
	q.closed = true
}

// stop waits for in-flight AddCode calls (via write lock), optionally cancels
// the forwarder, signals no more work, and waits for the forwarder to exit.
func (q *Queue) stop(shouldCancel bool) {
	q.markClosed()
	if shouldCancel {
		q.cancel()
	}
	q.closeInOnce.Do(func() {
		close(q.in)
	})
	<-q.forwardDone
}

// forward moves hashes from pending to `q.out`. It owns `q.out` and closes it on exit.
func (q *Queue) forward() {
	defer func() {
		close(q.out)
		close(q.forwardDone)
	}()
	for {
		select {
		case _, ok := <-q.in:
			stop := q.drainPending()
			if !ok || stop {
				return
			}
		case <-q.done:
			return
		}
	}
}

// drainPending sends all accumulated pending hashes to out.
// Returns true if cancelled via done.
func (q *Queue) drainPending() bool {
	takePending := func() []common.Hash {
		q.pendingMu.Lock()
		defer q.pendingMu.Unlock()
		batch := q.pending
		q.pending = nil
		return batch
	}

	for {
		batch := takePending()
		if len(batch) == 0 {
			return false
		}

		for _, h := range batch {
			select {
			case q.out <- h:
			case <-q.done:
				return true
			}
		}
	}
}

// init recovers persisted code markers from disk and re-enqueues them.
// AddCode will re-persist the same markers, which is a harmless redundancy
// that only happens on resume after restart.
func (q *Queue) init() error {
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return fmt.Errorf("unable to recover previous sync state: %w", err)
	}

	// context.Background: init runs during construction before sync starts,
	// the queue is not closed yet so AddCode will always succeed.
	if err := q.AddCode(context.Background(), dbCodeHashes); err != nil {
		return fmt.Errorf("unable to resume previous sync: %w", err)
	}

	return nil
}

// recoverUnfetchedCodeHashes returns persisted code markers that still need fetching
// and deletes markers for code already present locally.
func recoverUnfetchedCodeHashes(db ethdb.Database) ([]common.Hash, error) {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	batch := db.NewBatch()
	var codeHashes []common.Hash

	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])

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
