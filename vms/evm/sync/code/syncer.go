// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"

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
// A single manager goroutine owns every db read and write and the set of hashes
// being fetched, so batching, deduplication and persistence need no locking and
// see a consistent view. Only the network fetch runs in parallel, on
// numSyncWorkers goroutines, which is the part worth overlapping.
type Syncer struct {
	log        logging.Logger
	client     *Client
	db         ethdb.KeyValueStore
	codeHashes <-chan common.Hash

	codeHashesPerReq int // best-effort target size, the final batch may be smaller
}

// fetchResult carries a verified fetch back to the manager. The hashes travel
// with it because batches complete out of order.
type fetchResult struct {
	hashes []common.Hash
	data   [][]byte
}

// NewSyncer returns a [Syncer] that reads code hashes from codeHashes and writes
// verified code into db, fetching from peers through c.
func NewSyncer(log logging.Logger, c *Client, db ethdb.KeyValueStore, codeHashes <-chan common.Hash) *Syncer {
	return &Syncer{
		log:              log,
		client:           c,
		db:               db,
		codeHashes:       codeHashes,
		codeHashesPerReq: maxHashesPerRequest,
	}
}

// Sync runs until codeHashes is drained and closed, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	requests := make(chan []common.Hash)
	results := make(chan fetchResult)

	for range numSyncWorkers {
		eg.Go(func() error { return s.fetch(egCtx, requests, results) })
	}
	eg.Go(func() error {
		// Closing releases the fetchers once the manager stops handing out work.
		defer close(requests)
		return s.manage(egCtx, requests, results)
	})
	return eg.Wait()
}

// manage owns the db and the in-flight set. It batches incoming hashes, hands
// full batches to the fetchers, and persists what they return.
func (s *Syncer) manage(ctx context.Context, requests chan<- []common.Hash, results <-chan fetchResult) error {
	// Manager-local, so it needs no synchronisation.
	inFlight := make(map[common.Hash]struct{})

	var (
		batch   = make([]common.Hash, 0, s.codeHashesPerReq)
		pending int  // batches out with the fetchers
		drained bool // the queue is closed, so no more hashes are coming
	)
	for {
		if drained && len(batch) == 0 && pending == 0 {
			return nil
		}

		// Hand a batch off or keep filling one, never both, so a batch cannot
		// outgrow the cap the peer will accept.
		var (
			sendCh chan<- []common.Hash
			recvCh <-chan common.Hash
		)
		switch {
		case len(batch) > 0 && (len(batch) >= s.codeHashesPerReq || drained):
			sendCh = requests
		case !drained:
			recvCh = s.codeHashes
		}

		select {
		case <-ctx.Done():
			return ctx.Err()

		case sendCh <- batch:
			pending++
			// The fetcher owns the sent slice, so start a fresh one.
			batch = make([]common.Hash, 0, s.codeHashesPerReq)

		case res := <-results:
			pending--
			if err := s.persist(res.hashes, res.data); err != nil {
				return err
			}
			// Cleared only after the commit, so a hash arriving next is seen on
			// disk rather than fetched again.
			for _, codeHash := range res.hashes {
				delete(inFlight, codeHash)
			}

		case codeHash, ok := <-recvCh:
			if !ok {
				drained = true
				continue
			}
			missing, err := s.needsFetch(codeHash)
			if err != nil {
				return err
			}
			_, claimed := inFlight[codeHash]
			if !missing || claimed {
				continue
			}
			inFlight[codeHash] = struct{}{}
			batch = append(batch, codeHash)
		}
	}
}

// needsFetch reports whether codeHash has to come from a peer.
func (s *Syncer) needsFetch(codeHash common.Hash) (bool, error) {
	if !rawdb.HasCode(s.db, codeHash) {
		return true, nil
	}
	if err := customrawdb.DeleteCodeToFetch(s.db, codeHash); err != nil {
		return false, fmt.Errorf("failed to delete stale code marker: %w", err)
	}
	return false, nil
}

// fetch pulls batches, retrieves and verifies them, and reports back.
func (s *Syncer) fetch(ctx context.Context, requests <-chan []common.Hash, results chan<- fetchResult) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case hashes, ok := <-requests:
			if !ok {
				return nil
			}
			data, err := getCode(ctx, s.log, s.client, hashes)
			if err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- fetchResult{hashes: hashes, data: data}:
			}
		}
	}
}

// persist writes the code and clears the to-fetch markers in one batch.
func (s *Syncer) persist(hashes []common.Hash, data [][]byte) error {
	batch := s.db.NewBatch()
	for i, codeHash := range hashes {
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
		rawdb.WriteCode(batch, codeHash, data[i])
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write fetched code: %w", err)
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
		// Cheaper than hashing, and no valid blob can exceed this, so an
		// oversized one is rejected without paying for a keccak over it.
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
