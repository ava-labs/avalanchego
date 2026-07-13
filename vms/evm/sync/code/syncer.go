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
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/types"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const defaultNumWorkers = 5

var (
	_ types.Syncer = (*Syncer)(nil)

	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeSizeExceeded  = errors.New("max code size exceeded")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// Syncer fetches contract code by hash from the network and persists it to db.
// It consumes hashes from a channel, batches them, skips code already on disk,
// dedupes concurrent fetches, and clears the durable to-fetch marker for each
// hash it satisfies.
type Syncer struct {
	client     *Client
	db         ethdb.KeyValueStore
	codeHashes <-chan common.Hash

	numWorkers       int
	codeHashesPerReq int // best-effort target size, the final batch may be smaller

	// inFlight ensures only one worker fetches a given hash at a time.
	inFlight sync.Map
}

type syncerConfig struct {
	numWorkers       int
	codeHashesPerReq int
}

// SyncerOption configures a [Syncer] at construction time.
type SyncerOption = options.Option[syncerConfig]

// WithNumWorkers overrides the number of concurrent fetch workers.
func WithNumWorkers(n int) SyncerOption {
	return options.Func[syncerConfig](func(c *syncerConfig) {
		if n > 0 {
			c.numWorkers = n
		}
	})
}

// WithCodeHashesPerRequest overrides the best-effort batch size per request. It
// is capped at [MaxHashesPerRequest], the largest batch the handler accepts.
func WithCodeHashesPerRequest(n int) SyncerOption {
	return options.Func[syncerConfig](func(c *syncerConfig) {
		if n > 0 && n <= MaxHashesPerRequest {
			c.codeHashesPerReq = n
		}
	})
}

// NewSyncer returns a [Syncer] that reads code hashes from codeHashes and writes
// verified code into db, fetching from peers through c.
func NewSyncer(c *Client, db ethdb.KeyValueStore, codeHashes <-chan common.Hash, opts ...SyncerOption) *Syncer {
	cfg := syncerConfig{
		numWorkers:       defaultNumWorkers,
		codeHashesPerReq: MaxHashesPerRequest,
	}
	options.ApplyTo(&cfg, opts...)

	return &Syncer{
		client:           c,
		db:               db,
		codeHashes:       codeHashes,
		numWorkers:       cfg.numWorkers,
		codeHashesPerReq: cfg.codeHashesPerReq,
	}
}

// Name returns a human-readable name for logging.
func (*Syncer) Name() string { return "Code Syncer" }

// ID returns the stable identifier used for deduplication and metrics.
func (*Syncer) ID() string { return "state_code_sync" }

// Sync runs the workers until codeHashes is drained and closed, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for range s.numWorkers {
		eg.Go(func() error { return s.work(egCtx) })
	}
	return eg.Wait()
}

func (s *Syncer) work(ctx context.Context) error {
	batch := make([]common.Hash, 0, s.codeHashesPerReq)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case codeHash, ok := <-s.codeHashes:
			if !ok {
				if len(batch) > 0 {
					return s.fulfill(ctx, batch)
				}
				return nil
			}

			// Slow path: code already on disk, just clear its marker. Kept ahead
			// of the inFlight check so a marker a concurrent AddCode rewrote is
			// always re-cleaned on its next dequeue, never orphaned.
			if rawdb.HasCode(s.db, codeHash) {
				if err := s.clearMarker(codeHash); err != nil {
					return err
				}
				continue
			}

			// Fast path: dedupe concurrent fetches for the same hash.
			if _, loaded := s.inFlight.LoadOrStore(codeHash, struct{}{}); loaded {
				continue
			}

			batch = append(batch, codeHash)
			if len(batch) < s.codeHashesPerReq {
				continue
			}
			if err := s.fulfill(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
}

// fulfill fetches code for hashes, then writes it and clears the to-fetch
// markers in one batch.
func (s *Syncer) fulfill(ctx context.Context, hashes []common.Hash) error {
	data, err := getCode(ctx, s.client, hashes)
	if err != nil {
		return err
	}

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

	// Release ownership only after the write commits.
	for _, codeHash := range hashes {
		s.inFlight.Delete(codeHash)
	}
	return nil
}

// clearMarker deletes the to-fetch marker for a hash whose code is already local.
func (s *Syncer) clearMarker(codeHash common.Hash) error {
	batch := s.db.NewBatch()
	if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
		return fmt.Errorf("failed to delete stale code marker: %w", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch for stale code marker: %w", err)
	}
	return nil
}

// getCode fetches code for hashes and verifies each blob against its hash.
func getCode(ctx context.Context, c *Client, hashes []common.Hash) ([][]byte, error) {
	req := &syncpb.GetCodeRequest{Hashes: hashBytes(hashes)}
	resp, err := c.Send(ctx, req,
		func() *syncpb.GetCodeResponse { return &syncpb.GetCodeResponse{} },
		func(resp *syncpb.GetCodeResponse) error {
			return verifyCode(hashes, resp.GetData())
		},
	)
	if err != nil {
		return nil, err
	}
	return resp.GetData(), nil
}

// verifyCode reports whether data is the code for hashes, in order.
func verifyCode(hashes []common.Hash, data [][]byte) error {
	if len(data) != len(hashes) {
		return fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(data), len(hashes))
	}
	for i, code := range data {
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
