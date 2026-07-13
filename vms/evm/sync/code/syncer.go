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
	"github.com/ava-labs/avalanchego/vms/evm/sync/types"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const numSyncWorkers = 5

var (
	_ types.Syncer = (*Syncer)(nil)

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
	log    logging.Logger
	client *Client
	db     ethdb.KeyValueStore
	q      *queue

	started atomic.Bool
}

// NewSyncer returns a [Syncer] that writes verified code into db, fetching from
// peers through c. An interrupted sync resumes from the markers it left.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore) (*Syncer, error) {
	s := &Syncer{
		log:    log,
		client: c,
		db:     db,
		q:      newQueue(),
	}
	if err := s.requeueOutstanding(); err != nil {
		return nil, err
	}
	return s, nil
}

// AddCode marks hashes as outstanding and queues them, skipping code already on
// disk. Never waits on the fetchers.
func (s *Syncer) AddCode(ctx context.Context, hashes []common.Hash) error {
	if len(hashes) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	batch := s.db.NewBatch()
	missing := make([]common.Hash, 0, len(hashes))
	for _, codeHash := range hashes {
		if rawdb.HasCode(s.db, codeHash) {
			continue
		}
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("marking code to fetch: %w", err)
		}
		missing = append(missing, codeHash)
	}

	// One step against CloseInput, so a refused call leaves nothing owed.
	return s.q.admit(missing, func() error {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("committing code to fetch markers: %w", err)
		}
		return nil
	})
}

// CloseInput stops taking hashes. [Syncer.Sync] returns once the queue drains.
// Safe from any goroutine, more than once, and before Sync.
func (s *Syncer) CloseInput() {
	s.q.close()
}

// Name returns a human-readable name for logging.
func (*Syncer) Name() string { return "Code Syncer" }

// ID returns the stable identifier used for deduplication and metrics.
func (*Syncer) ID() string { return "state_code_sync" }

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

// requeueOutstanding re-queues the markers a previous run left, clearing those
// whose code has since arrived. Runs before any producer holds the Syncer.
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
			outstanding = append(outstanding, codeHash)
			continue
		}
		// A near-complete sync clears a marker per contract, too many for one batch.
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

	s.q.enqueue(outstanding)
	return nil
}

// batchHashes drains the queue, handing each full batch to a worker.
func (s *Syncer) batchHashes(ctx context.Context, eg *errgroup.Group) error {
	claimed := &claimSet{}
	batch := make([]common.Hash, 0, maxHashesPerRequest)
	fetch := func() {
		full := batch
		eg.Go(func() error {
			return s.fetchAndPersist(ctx, full, claimed)
		})
		batch = make([]common.Hash, 0, maxHashesPerRequest)
	}

	for {
		queued, closed := s.q.take()
		for _, codeHash := range queued {
			// Claim first, so a repeat cannot read the code missing and then claim
			// it as the commit lands. Cleanup runs either way.
			alreadyClaimed := !claimed.claim(codeHash)
			stored, err := clearIfStored(s.db, s.db, codeHash)
			if err != nil {
				return err
			}
			if alreadyClaimed {
				continue
			}
			if stored {
				claimed.release(codeHash)
				continue
			}

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
		// More may have arrived while this drain ran.
		if len(queued) > 0 {
			continue
		}

		if err := s.q.wait(ctx); err != nil {
			return err
		}
	}
}

// fetchAndPersist fetches code for hashes from the network and writes it to db.
func (s *Syncer) fetchAndPersist(ctx context.Context, hashes []common.Hash, claimed *claimSet) error {
	data, err := getCode(ctx, s.log, s.client, hashes)
	if err != nil {
		return err
	}
	if err := persist(s.db, hashes, data); err != nil {
		return err
	}
	// Released only after the commit, so a repeat is seen on disk instead.
	claimed.release(hashes...)
	return nil
}

// claimSet holds the hashes a batch has taken, until their code is committed, so
// a repeat is not fetched twice. Bounded by the work outstanding.
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

// getCode requests hashes through c and verifies every blob against its hash,
// scoring the peer. Re-requests until ctx ends.
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
			log.Debug("code request failed, re-requesting", zap.Error(err))
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
		// Size is deliberately not checked. Contracts larger than MaxCodeSize
		// exist, and rejecting one would reject the only answer a peer can give.
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
