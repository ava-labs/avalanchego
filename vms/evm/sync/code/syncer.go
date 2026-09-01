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

	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Syncer resolves contract code by hash from peers.
type Syncer struct {
	log    logging.Logger
	client *Client
	db     ethdb.KeyValueStore

	claimed claimSet
	started atomic.Bool

	lock       sync.Mutex
	cond       *lock.Cond
	doneAdding bool
	queued     []common.Hash
}

// NewSyncer returns a [Syncer] that downloads contract code through c and
// writes it to db. A restarted sync resumes where the previous one left off.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore) (*Syncer, error) {
	s := &Syncer{
		log:    log,
		client: c,
		db:     db,
	}
	s.cond = lock.NewCond(&s.lock)
	if err := s.requeueOutstanding(); err != nil {
		return nil, err
	}
	return s, nil
}

// requeueOutstanding re-enqueues the code hashes a previous run marked but
// never fetched, and deletes markers whose code is already on disk. It must
// run before the Syncer is shared with other goroutines.
func (s *Syncer) requeueOutstanding() error {
	it := customrawdb.NewCodeToFetchIterator(s.db)
	defer it.Release()

	batch := s.db.NewBatch()
	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		if !rawdb.HasCode(s.db, codeHash) {
			s.claimed.claim(codeHash)
			s.queued = append(s.queued, codeHash)
			continue
		}

		// Another part of the node already wrote this code, so the fetch is
		// no longer needed.
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("deleting stale code marker: %w", err)
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterating code to fetch markers: %w", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("committing recovered marker clears: %w", err)
	}
	return nil
}

var errDoneAdding = errors.New("code syncer is no longer accepting hashes")

// AddCode marks code hashes to be fetched during syncing. If AddCode returns
// nil, the code will be populated by the time [Syncer.Sync] returns nil, even
// if the node crashes and the syncer restarts.
//
// AddCode NEVER blocks on the network, so it is safe to call from the VM's app
// message handlers. If it did, those handlers could deadlock with the syncer's
// own code requests.
func (s *Syncer) AddCode(hashes []common.Hash) (retErr error) {
	batch := s.db.NewBatch()
	missing := make([]common.Hash, 0, len(hashes))
	// On error the missing hashes are never enqueued, so the fetcher will
	// never release them. Release them here instead.
	defer func() {
		if retErr != nil {
			s.claimed.release(missing...)
		}
	}()

	for _, codeHash := range hashes {
		// Claiming gives this goroutine sole ownership of the hash. Every
		// successful claim MUST eventually be released.
		if !s.claimed.claim(codeHash) {
			continue
		}
		// Skip code that is already on disk.
		if rawdb.HasCode(s.db, codeHash) {
			s.claimed.release(codeHash)
			continue
		}
		missing = append(missing, codeHash)
		// Once AddCode returns, the account syncer will persist accounts that
		// reference this code. The marker survives a crash, so a restarted
		// sync still fetches the code.
		if err := customrawdb.WriteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("marking code to fetch: %w", err)
		}
	}

	// The lock is taken only now so the DB reads above run outside of it.
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.doneAdding {
		return errDoneAdding
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("committing code to fetch markers: %w", err)
	}

	s.queued = append(s.queued, missing...)
	s.cond.Broadcast()
	return nil
}

// DoneAdding stops [Syncer.AddCode] from accepting new hashes and allows
// [Syncer.Sync] to return once it finishes fetching the queued hashes.
//
// DoneAdding is idempotent and safe to call at any time.
func (s *Syncer) DoneAdding() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.doneAdding = true
	s.cond.Broadcast()
}

var errSyncAlreadyRun = errors.New("code syncer has already run")

// Sync runs until [Syncer.DoneAdding] has been called and every hash given to
// [Syncer.AddCode] has its code on disk, or until ctx is cancelled.
//
// Sync MUST be called at most once. A cancelled Sync leaves hashes claimed but
// unfetched, and only the recovery in [NewSyncer] re-enqueues them, so each
// attempt needs a fresh Syncer.
func (s *Syncer) Sync(ctx context.Context) error {
	if !s.started.CompareAndSwap(false, true) {
		return errSyncAlreadyRun
	}

	eg, egCtx := errgroup.WithContext(ctx)
	const numCodeFetchers = 5
	// One extra slot for the batcher, so numCodeFetchers can run alongside it.
	eg.SetLimit(numCodeFetchers + 1)

	eg.Go(func() error { return s.batchHashes(egCtx, eg) })
	return eg.Wait()
}

// drainQueue blocks until at least one hash is queued or [Syncer.DoneAdding]
// has been called, returning the queued hashes and whether adding is done.
func (s *Syncer) drainQueue(ctx context.Context) ([]common.Hash, bool, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for len(s.queued) == 0 && !s.doneAdding {
		if err := s.cond.Wait(ctx); err != nil {
			return nil, false, err
		}
	}

	pending := s.queued
	s.queued = nil
	return pending, s.doneAdding, nil
}

// batchHashes drains the queue, handing each batch to a worker. Every dequeued
// hash arrives already claimed by AddCode or recovery.
func (s *Syncer) batchHashes(ctx context.Context, eg *errgroup.Group) error {
	batch := make([]common.Hash, 0, maxHashesPerRequest)
	fetch := func() {
		hashes := batch
		eg.Go(func() error {
			return s.fetchAndPersist(ctx, hashes)
		})
		batch = make([]common.Hash, 0, maxHashesPerRequest)
	}

	for {
		queued, done, err := s.drainQueue(ctx)
		if err != nil {
			return err
		}

		for _, codeHash := range queued {
			batch = append(batch, codeHash)
			if len(batch) == maxHashesPerRequest {
				fetch()
			}
		}

		if done {
			if len(batch) > 0 {
				fetch()
			}
			return nil
		}
	}
}

// fetchAndPersist downloads the code for hashes and writes it to disk.
func (s *Syncer) fetchAndPersist(ctx context.Context, hashes []common.Hash) error {
	data, err := getCode(ctx, s.log, s.client, hashes)
	if err != nil {
		return err
	}
	if err := persist(s.db, hashes, data); err != nil {
		return err
	}
	// Released only after the commit, so a repeated hash finds the code on
	// disk instead of fetching it again.
	s.claimed.release(hashes...)
	return nil
}

// claimSet holds each hash from acceptance until its code is committed, so a
// repeated hash is not fetched twice. Its size is bounded by the outstanding
// work.
type claimSet struct {
	m sync.Map
}

// claim takes codeHash, returning true if it was successfully claimed.
func (c *claimSet) claim(codeHash common.Hash) bool {
	_, held := c.m.LoadOrStore(codeHash, struct{}{})
	return !held
}

// release clears the claims on hashes.
func (c *claimSet) release(hashes ...common.Hash) {
	for _, codeHash := range hashes {
		c.m.Delete(codeHash)
	}
}

// persist writes the code and clears the to-fetch markers in one batch.
func persist(db ethdb.Batcher, hashes []common.Hash, codes [][]byte) error {
	batch := db.NewBatch()
	for i, codeHash := range hashes {
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("deleting code to fetch marker: %w", err)
		}
		rawdb.WriteCode(batch, codeHash, codes[i])
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("writing fetched code: %w", err)
	}
	return nil
}

// getCode fetches the code for hashes through c, scoring each peer on its
// response. It retries until a peer returns valid code or ctx is cancelled.
func getCode(ctx context.Context, log logging.Logger, c *Client, hashes []common.Hash) ([][]byte, error) {
	req := &syncpb.GetCodeRequest{Hashes: hashBytes(hashes)}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp := &syncpb.GetCodeResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored any peer it reached, re-request.
			log.Debug("code request failed, re-requesting",
				zap.Error(err),
			)
			continue
		}

		codes := resp.GetData()
		if err := verifyCode(hashes, codes); err != nil {
			outcome.Failure()
			log.Debug("invalid code response, re-requesting",
				zap.Stringer("nodeID", outcome.NodeID()),
				zap.Error(err),
			)
			continue
		}

		outcome.Success()
		outcome.MarkReceived(len(codes))
		return codes, nil
	}
}

var (
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// verifyCode checks that codes match hashes, in count and in content.
func verifyCode(hashes []common.Hash, codes [][]byte) error {
	if len(codes) != len(hashes) {
		return fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(codes), len(hashes))
	}
	for i, code := range codes {
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
