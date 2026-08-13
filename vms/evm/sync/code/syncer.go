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
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const numSyncWorkers = 5

var (
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeSizeExceeded  = errors.New("max code size exceeded")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
	errUnexpectedCode    = errors.New("unexpected code")
)

// Syncer fetches contract code by hash from the network and persists it to db.
//
// One goroutine batches queued hashes. Up to numSyncWorkers others each fetch,
// verify and store a batch, so the round trips overlap.
type Syncer struct {
	log    logging.Logger
	client *Client
	db     ethdb.KeyValueStore

	trackedlock   sync.Mutex
	trackedHashes set.Set[common.Hash]

	// closeLock guards closed, so a signal cannot race the close.
	closeLock sync.Mutex
	closed    bool

	hasPendingHashes chan struct{}
	pendingLock      sync.Mutex
	pendingHashes    []common.Hash
}

// NewSyncer returns a [Syncer] that fetches code from peers through c and
// writes it, verified, into db. Markers persisted by an earlier run are
// re-enqueued, so a crashed sync resumes where it stopped.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore) (*Syncer, error) {
	codeToFetch, err := readCodeToFetch(db)
	if err != nil {
		return nil, err
	}

	ch := make(chan struct{}, 1)
	ch <- struct{}{} // Initially process the code from disk
	return &Syncer{
		log:              log,
		client:           c,
		db:               db,
		trackedHashes:    set.Of(codeToFetch...),
		hasPendingHashes: ch,
		pendingHashes:    codeToFetch,
	}, nil
}

// AddCode persists a durable marker for each hash not already being synced and
// enqueues it. It never blocks. A call with more=false closes the queue, and
// every call after that fails.
func (s *Syncer) AddCode(codeHashes []common.Hash, more bool) error {
	toSync := s.track(codeHashes)
	if err := writeToFetch(s.db, toSync); err != nil {
		return err
	}
	s.enqueue(toSync)

	s.closeLock.Lock()
	defer s.closeLock.Unlock()

	if s.closed {
		return errUnexpectedCode
	}

	if more {
		select {
		case s.hasPendingHashes <- struct{}{}:
		default:
		}
		return nil
	}

	s.closed = true
	close(s.hasPendingHashes)
	return nil
}

// track adds the codeHashes to the in-memory set of hashes. It returns the
// subset of hashes that were not already tracked.
func (s *Syncer) track(codeHashes []common.Hash) []common.Hash {
	toSync := make([]common.Hash, 0, len(codeHashes))

	s.trackedlock.Lock()
	defer s.trackedlock.Unlock()

	for _, codeHash := range codeHashes {
		if s.trackedHashes.Contains(codeHash) {
			continue
		}
		s.trackedHashes.Add(codeHash)
		toSync = append(toSync, codeHash)
	}

	return toSync
}

// untrack removes the codeHashes from the in-memory set of hashes.
func (s *Syncer) untrack(codeHashes []common.Hash) {
	s.trackedlock.Lock()
	defer s.trackedlock.Unlock()

	for _, codeHash := range codeHashes {
		s.trackedHashes.Remove(codeHash)
	}
}

// enqueue adds codeHashes to the queue of hashes for the batcher to consume.
func (s *Syncer) enqueue(codeHashes []common.Hash) {
	s.pendingLock.Lock()
	defer s.pendingLock.Unlock()

	s.pendingHashes = append(s.pendingHashes, codeHashes...)
}

// dequeue removes and returns all queued codeHashes.
func (s *Syncer) dequeue() []common.Hash {
	s.pendingLock.Lock()
	defer s.pendingLock.Unlock()

	codeHashes := s.pendingHashes
	s.pendingHashes = nil
	return codeHashes
}

// Sync runs until the queue is drained and closed by [Syncer.AddCode], or ctx
// ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	// The batcher occupies a slot of its own, so numSyncWorkers fetches still
	// run alongside it.
	eg.SetLimit(numSyncWorkers + 1)

	eg.Go(func() error { return s.batchHashes(egCtx, eg) })
	return eg.Wait()
}

// batchHashes drains the queue, handing every batch to a worker. The last one
// is short unless the queue divides evenly.
func (s *Syncer) batchHashes(ctx context.Context, eg *errgroup.Group) error {
	var queued []common.Hash
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case _, more := <-s.hasPendingHashes:
			// [Syncer.AddCode] doesn't check for the presence of code before
			// writing the markers, to avoid the DB read latency hit. So we
			// filter out any hashes that are already in the DB here before
			// sending any network requests for them.
			missing, err := clearStored(s.db, s.dequeue())
			if err != nil {
				return err
			}
			queued = append(queued, missing...)

			for len(queued) >= maxHashesPerRequest {
				full := queued[:maxHashesPerRequest]
				queued = queued[maxHashesPerRequest:]
				eg.Go(func() error {
					return s.fetchAndPersist(ctx, full)
				})
			}
			if more {
				continue
			}
			if len(queued) > 0 {
				eg.Go(func() error {
					return s.fetchAndPersist(ctx, queued)
				})
			}
			return nil
		}
	}
}

// fetchAndPersist fetches code for hashes from the network and writes it to db.
func (s *Syncer) fetchAndPersist(ctx context.Context, hashes []common.Hash) error {
	data, err := getCode(ctx, s.log, s.client, hashes)
	if err != nil {
		return err
	}
	if err := persist(s.db, hashes, data); err != nil {
		return err
	}
	s.untrack(hashes)
	return nil
}

// readCodeToFetch returns the hashes whose to-fetch markers are persisted in
// db.
func readCodeToFetch(db ethdb.Iteratee) ([]common.Hash, error) {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	var codeHashes []common.Hash
	for it.Next() {
		codeHashes = append(
			codeHashes,
			common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):]),
		)
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("iterating code to fetch: %w", err)
	}
	return codeHashes, nil
}

// writeToFetch writes, in one batch, a marker for each codeHash to fetch.
func writeToFetch(db ethdb.KeyValueStore, codeHashes []common.Hash) error {
	batch := db.NewBatch()
	for _, codeHash := range codeHashes {
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("writing code to fetch marker: %w", err)
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("writing code to fetch: %w", err)
	}
	return nil
}

// clearStored deletes, in one batch, the markers of hashes whose code is
// already in db, and returns the hashes still missing.
func clearStored(db ethdb.KeyValueStore, codeHashes []common.Hash) ([]common.Hash, error) {
	var (
		missing = make([]common.Hash, 0, len(codeHashes))
		batch   = db.NewBatch()
	)
	// TODO(StephenButtolph): This is a read-heavy operation, so it may be worth
	// parallelizing the HasCode checks.
	for _, codeHash := range codeHashes {
		if !rawdb.HasCode(db, codeHash) {
			missing = append(missing, codeHash)
			continue
		}
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return nil, fmt.Errorf("deleting stale code marker: %w", err)
		}
	}
	if batch.ValueSize() == 0 {
		return missing, nil
	}
	if err := batch.Write(); err != nil {
		return nil, fmt.Errorf("deleting stale code markers: %w", err)
	}
	return missing, nil
}

// persist writes the code and clears the to-fetch markers in one batch.
func persist(db ethdb.Batcher, hashes []common.Hash, data [][]byte) error {
	batch := db.NewBatch()
	for i, codeHash := range hashes {
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("deleting code to fetch marker: %w", err)
		}
		rawdb.WriteCode(batch, codeHash, data[i])
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("writing fetched code: %w", err)
	}
	return nil
}

// getCode requests hashes through c, verifies every returned blob against its
// hash, and scores the peer. It re-requests on any network or verification
// failure until ctx ends.
func getCode(ctx context.Context, log logging.Logger, c *Client, hashes []common.Hash) ([][]byte, error) {
	req := &syncpb.GetCodeRequest{Hashes: hashBytes(hashes)}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp := &syncpb.GetCodeResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		data := resp.GetData()
		if err := verifyCode(hashes, data); err != nil {
			outcome.Failure()
			log.Debug("invalid code response, re-requesting",
				zap.Stringer("nodeID", outcome.NodeID()),
				zap.Error(err),
			)
			continue
		}

		outcome.Success()
		return data, nil
	}
}

// verifyCode reports whether data is the code for hashes, in order.
func verifyCode(hashes []common.Hash, data [][]byte) error {
	if len(data) != len(hashes) {
		return fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(data), len(hashes))
	}
	for i, code := range data {
		// Not needed for correctness, since an oversized blob cannot hash to
		// what was asked for. It bounds the work a peer can force on us to one
		// keccak over MaxCodeSize rather than one over the whole message.
		if len(code) > params.MaxCodeSize {
			return fmt.Errorf("%w: hash %s size %d", errCodeSizeExceeded, hashes[i], len(code))
		}
		if got := crypto.Keccak256Hash(code); got != hashes[i] {
			return fmt.Errorf("%w at index %d: got %s requested %s", errCodeHashMismatch, i, got, hashes[i])
		}
	}
	return nil
}

func hashBytes(hashes []common.Hash) [][]byte {
	raw := make([][]byte, len(hashes))
	for i, h := range hashes {
		raw[i] = h.Bytes()
	}
	return raw
}
