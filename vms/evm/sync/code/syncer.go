// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	numSyncWorkers = 5

	// defaultIntakeBound caps outstanding hashes before intake waits.
	defaultIntakeBound = 100_000
)

var (
	// ErrInputClosed reports that nothing will fetch what the caller is offering.
	ErrInputClosed = errors.New("code syncer input is closed")

	errSyncAlreadyRun    = errors.New("code syncer has already run")
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// Syncer resolves contract code by hash, and owns every write to both the
// bytecode and the to-fetch marker.
//
// One goroutine batches queued hashes, numSyncWorkers others fetch.
type Syncer struct {
	log     logging.Logger
	client  *Client
	db      ethdb.KeyValueStore
	q       *queue
	claimed *claimSet

	started atomic.Bool
}

// NewSyncer returns a [Syncer] that writes verified code into db, fetching from
// peers through c. An interrupted sync resumes from the markers it left.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore) (*Syncer, error) {
	return newSyncer(log, c, db, defaultIntakeBound)
}

// newSyncer is [NewSyncer] with the intake bound exposed, so a test can reach a
// value small enough to engage.
func newSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore, bound int) (*Syncer, error) {
	s := &Syncer{
		log:     log,
		client:  c,
		db:      db,
		q:       newQueue(bound),
		claimed: &claimSet{},
	}
	if err := s.requeueOutstanding(); err != nil {
		return nil, err
	}
	return s, nil
}

// AddCode marks hashes as outstanding and queues them, skipping code already
// stored or claimed by a repeat in flight. Waits while intake is full.
func (s *Syncer) AddCode(ctx context.Context, hashes []common.Hash) (retErr error) {
	if len(hashes) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Reserved before the gate, since waiting inside it would block close, and
	// for the whole call, since what survives the filter is not known yet.
	reserved := len(hashes)
	if err := s.q.reserve(ctx, reserved); err != nil {
		return err
	}
	// Only a failure keeps the whole reservation. The enqueue below consumes
	// what survived and hands the rest back.
	defer func() {
		if retErr != nil {
			s.q.release(reserved)
		}
	}()

	// Held across the claim, the write and the enqueue, so a refused call leaves
	// nothing owed and no repeat skips a hash this call then fails to queue.
	if !s.q.enter() {
		return ErrInputClosed
	}
	defer s.q.exit()

	batch := s.db.NewBatch()
	missing := make([]common.Hash, 0, len(hashes))
	// Released on any error, so a failed call leaves nothing claimed with no
	// fetch coming for it.
	defer func() {
		if retErr != nil {
			s.claimed.release(missing...)
		}
	}()

	for _, codeHash := range hashes {
		if rawdb.HasCode(s.db, codeHash) {
			continue
		}
		// Claim before writing, so a repeat already claimed is skipped here
		// instead of writing a duplicate marker or queuing a duplicate fetch.
		if !s.claimed.claim(codeHash) {
			continue
		}
		missing = append(missing, codeHash)
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("marking code to fetch: %w", err)
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("committing code to fetch markers: %w", err)
	}

	s.q.enqueue(missing, reserved)
	return nil
}

// CloseInput stops taking hashes. [Syncer.Sync] returns once the queue drains.
// Safe from any goroutine, more than once, and before Sync.
func (s *Syncer) CloseInput() {
	s.q.close()
}

// Sync fetches until input closes and the queue drains, or ctx ends. Runs once.
func (s *Syncer) Sync(ctx context.Context) error {
	if !s.started.CompareAndSwap(false, true) {
		return errSyncAlreadyRun
	}
	// Stopped consuming, so close input.
	defer s.CloseInput()

	eg, egCtx := errgroup.WithContext(ctx)
	// A slot for the batcher, so numSyncWorkers fetches still run alongside it.
	eg.SetLimit(numSyncWorkers + 1)

	eg.Go(func() error { return s.batchHashes(egCtx, eg) })
	return eg.Wait()
}

// requeueOutstanding re-queues markers a previous run left, clearing ones
// StateDB.Commit already satisfied. Runs before any producer holds the Syncer.
func (s *Syncer) requeueOutstanding() error {
	it := customrawdb.NewCodeToFetchIterator(s.db)
	defer it.Release()

	batch := s.db.NewBatch()
	var outstanding []common.Hash
	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		stored, err := clearIfStored(s.db, batch, codeHash)
		if err != nil {
			return err
		}
		if !stored {
			// Claimed here too, so a concurrent AddCode for the same hash defers
			// to whichever of the two reaches the queue.
			s.claimed.claim(codeHash)
			outstanding = append(outstanding, codeHash)
			continue
		}
		// Resuming after many blocks executed locally can satisfy most markers
		// this way, too many clears for one batch.
		if batch.ValueSize() < ethdb.IdealBatchSize {
			continue
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("committing recovered marker clears: %w", err)
		}
		batch.Reset()
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterating code to fetch markers: %w", err)
	}
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("committing recovered marker clears: %w", err)
		}
	}

	s.q.enqueue(outstanding, 0)
	return nil
}

// batchHashes drains the queue, handing each full batch to a worker. Every
// dequeued hash arrives already claimed by AddCode or recovery.
func (s *Syncer) batchHashes(ctx context.Context, eg *errgroup.Group) error {
	batch := make([]common.Hash, 0, maxHashesPerRequest)
	fetch := func() {
		full := batch
		eg.Go(func() error {
			return s.fetchAndPersist(ctx, full)
		})
		batch = make([]common.Hash, 0, maxHashesPerRequest)
	}

	for {
		queued, closed := s.q.take()
		for _, codeHash := range queued {
			batch = append(batch, codeHash)
			if len(batch) == maxHashesPerRequest {
				fetch()
			}
		}

		if closed && len(queued) == 0 {
			if len(batch) > 0 {
				fetch()
			}
			return nil
		}

		// A wakeup already pending from work added mid-drain returns at once, so
		// this never waits past hashes that arrived while queued was processed.
		if err := s.q.wait(ctx); err != nil {
			return err
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
	// Released only after the commit, so a repeat is seen on disk instead.
	s.claimed.release(hashes...)
	return nil
}

// claimSet holds the hashes accepted into the pipeline, until their code is
// committed, so a repeat is not fetched twice. Bounded by the work outstanding.
type claimSet struct {
	m sync.Map
}

// claim reports whether codeHash was taken, and false if it was already held.
func (c *claimSet) claim(codeHash common.Hash) bool {
	_, held := c.m.LoadOrStore(codeHash, struct{}{})
	return !held
}

func (c *claimSet) release(hashes ...common.Hash) {
	for _, codeHash := range hashes {
		c.m.Delete(codeHash)
	}
}

// clearIfStored deletes the to-fetch marker through w if the code is on disk,
// reporting whether it was.
func clearIfStored(r ethdb.KeyValueReader, w ethdb.KeyValueWriter, codeHash common.Hash) (bool, error) {
	if !rawdb.HasCode(r, codeHash) {
		return false, nil
	}
	if err := customrawdb.DeleteCodeToFetch(w, codeHash); err != nil {
		return false, fmt.Errorf("deleting stale code marker: %w", err)
	}
	return true, nil
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

// getCode requests hashes through c, resuming for whatever a partial response
// leaves out. Scores the peer per request, and fails fast on an unfetchable hash.
func getCode(ctx context.Context, log logging.Logger, c *Client, hashes []common.Hash) ([][]byte, error) {
	data := make([][]byte, 0, len(hashes))
	remaining := hashes
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req := &syncpb.GetCodeRequest{Hashes: hashBytes(remaining)}
		resp := &syncpb.GetCodeResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if errors.Is(err, errCodeTooLarge) {
			return nil, fmt.Errorf("%w: %s", errCodeTooLarge, remaining[0])
		}
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			log.Debug("code request failed, re-requesting", zap.Error(err))
			continue
		}

		got := resp.GetData()
		n, err := verifyCodePrefix(remaining, got)
		if err != nil {
			outcome.Failure()
			log.Debug("invalid code response, re-requesting",
				zap.Stringer("nodeID", outcome.NodeID()),
				zap.Error(err),
			)
			continue
		}

		outcome.Success()
		data = append(data, got...)
		remaining = remaining[n:]
	}
	return data, nil
}

// verifyCodePrefix reports how many leading hashes data accounts for, which may
// be fewer than requested when the rest would not fit in one message.
func verifyCodePrefix(hashes []common.Hash, data [][]byte) (int, error) {
	if len(data) == 0 || len(data) > len(hashes) {
		return 0, fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(data), len(hashes))
	}
	for i, code := range data {
		if got := crypto.Keccak256Hash(code); got != hashes[i] {
			return 0, fmt.Errorf("%w at index %d: got %s requested %s", errCodeHashMismatch, i, got, hashes[i])
		}
	}
	return len(data), nil
}

func hashBytes(hashes []common.Hash) [][]byte {
	raw := make([][]byte, len(hashes))
	for i, h := range hashes {
		raw[i] = h.Bytes()
	}
	return raw
}
