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
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const numSyncWorkers = 5

var (
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeSizeExceeded  = errors.New("max code size exceeded")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// Syncer fetches contract code by hash from the network and persists it to db.
//
// One goroutine batches queued hashes. Up to numSyncWorkers others each fetch,
// verify and store a batch, so the round trips overlap.
type Syncer struct {
	log        logging.Logger
	client     *Client
	db         ethdb.KeyValueStore
	codeHashes <-chan common.Hash
}

// NewSyncer returns a [Syncer] that reads code hashes from codeHashes and writes
// verified code into db, fetching from peers through c.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore, codeHashes <-chan common.Hash) *Syncer {
	return &Syncer{
		log:        log,
		client:     c,
		db:         db,
		codeHashes: codeHashes,
	}
}

// Sync runs until codeHashes is drained and closed, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	// The batcher occupies a slot of its own, so numSyncWorkers fetches still run
	// alongside it.
	eg.SetLimit(numSyncWorkers + 1)

	eg.Go(func() error { return s.batchHashes(egCtx, eg) })
	return eg.Wait()
}

// batchHashes drains the queue, handing every batch to a worker. The last one is
// short unless the queue divides evenly.
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
		select {
		case <-ctx.Done():
			return ctx.Err()

		case codeHash, ok := <-s.codeHashes:
			if !ok {
				if len(batch) > 0 {
					fetch()
				}
				return nil
			}
			// Claim first, so a repeat cannot read the code missing and then claim
			// it as the commit lands. Cleanup runs either way, since a hash
			// re-marked mid-commit is only cleared by a later copy.
			alreadyClaimed := !claimed.claim(codeHash)
			stored, err := clearIfStored(s.db, codeHash)
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
	// Released only after the commit, so a repeat arriving next is seen on disk
	// rather than fetched again.
	claimed.release(hashes...)
	return nil
}

// claimSet holds the hashes a batch has taken, from the moment batchHashes picks one
// until its code is committed, so a repeat is not fetched twice. It is bounded by
// the work outstanding, not by the hashes seen.
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

// clearIfStored reports whether the code is already on disk, deleting its
// to-fetch marker when it is.
func clearIfStored(db ethdb.KeyValueStore, codeHash common.Hash) (bool, error) {
	if !rawdb.HasCode(db, codeHash) {
		return false, nil
	}
	if err := customrawdb.DeleteCodeToFetch(db, codeHash); err != nil {
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
