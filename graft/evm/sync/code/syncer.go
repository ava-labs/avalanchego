// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

const (
	defaultNumCodeFetchingWorkers = 5

	// SyncerID is the stable identifier for the code syncer.
	SyncerID = "state_code_sync"
)

var _ types.Syncer = (*Syncer)(nil)

// Syncer syncs code bytes from the network in a separate thread.
// It consumes code hashes from a queue and persists code into the DB.
// Outstanding requests are tracked via durable "to-fetch" markers in the DB for recovery.
// The syncer performs in-flight deduplication and skips locally-present code before issuing requests.
type Syncer struct {
	db     ethdb.Database
	client client.Client

	// Channel of incoming code hash requests provided by the fetcher.
	codeHashes <-chan common.Hash

	// Config options.
	numWorkers       int
	codeHashesPerReq int // best-effort target size, final batch may be smaller

	// inFlight tracks code hashes currently being processed to dedupe work
	// across workers and across repeated queue submissions.
	inFlight sync.Map // key: common.Hash, value: struct{}
}

// syncerConfig carries construction-time options for the code syncer.
type syncerConfig struct {
	numWorkers       int
	codeHashesPerReq int
}

// SyncerOption configures the code syncer at construction time.
type SyncerOption = options.Option[syncerConfig]

// WithNumWorkers overrides the number of concurrent workers.
func WithNumWorkers(n int) SyncerOption {
	return options.Func[syncerConfig](func(c *syncerConfig) {
		if n > 0 {
			c.numWorkers = n
		}
	})
}

// WithCodeHashesPerRequest sets the best-effort target batch size per request.
// The final batch may contain fewer than the configured number if insufficient
// hashes remain when the channel is closed.
func WithCodeHashesPerRequest(n int) SyncerOption {
	return options.Func[syncerConfig](func(c *syncerConfig) {
		if n > 0 {
			c.codeHashesPerReq = n
		}
	})
}

func NewSyncer(client client.Client, db ethdb.Database, codeHashes <-chan common.Hash, opts ...SyncerOption) (*Syncer, error) {
	cfg := syncerConfig{
		numWorkers:       defaultNumCodeFetchingWorkers,
		codeHashesPerReq: message.MaxCodeHashesPerRequest,
	}
	options.ApplyTo(&cfg, opts...)

	return &Syncer{
		db:               db,
		client:           client,
		codeHashes:       codeHashes,
		numWorkers:       cfg.numWorkers,
		codeHashesPerReq: cfg.codeHashesPerReq,
	}, nil
}

// Name returns the human-readable name for this sync task.
func (*Syncer) Name() string { return "Code Syncer" }

// ID returns the stable identifier for this sync task.
func (*Syncer) ID() string { return SyncerID }

// Sync starts the worker threads and blocks until all outstanding code
// requests have been fetched and the code channel has been closed, or
// the context is cancelled.
func (c *Syncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for range c.numWorkers {
		eg.Go(func() error { return c.work(egCtx, c.codeHashes) })
	}
	return eg.Wait()
}

func (*Syncer) UpdateTarget(message.Syncable) error {
	return nil
}

func (c *Syncer) releaseInFlight(codeHashes []common.Hash) {
	for _, h := range codeHashes {
		c.inFlight.Delete(h)
	}
}

// work fulfills incoming requests from the producer channel by fetching code
// bytes from the network and persisting them to the database.
func (c *Syncer) work(ctx context.Context, codeHashesCh <-chan common.Hash) error {
	codeHashes := make([]common.Hash, 0, message.MaxCodeHashesPerRequest)

	for {
		select {
		case <-ctx.Done(): // If ctx is done, set the error to the ctx error since work has been cancelled.
			c.releaseInFlight(codeHashes)
			return ctx.Err()
		case codeHash, ok := <-codeHashesCh:
			// If there are no more [codeHashes], fulfill a last code request for any [codeHashes] previously
			// read from the channel, then return.
			if !ok {
				if len(codeHashes) > 0 {
					return c.fulfillCodeRequest(ctx, codeHashes)
				}
				return nil
			}

			// Deduplicate in-flight code hashes across workers first to avoid
			// racing repeated HasCode() checks for the same hash.
			if _, loaded := c.inFlight.LoadOrStore(codeHash, struct{}{}); loaded {
				continue
			}

			// After acquiring responsibility for this hash, re-check whether the code
			// is already present locally. If so, clean up and release responsibility.
			if rawdb.HasCode(c.db, codeHash) {
				// Best-effort cleanup of stale marker.
				batch := c.db.NewBatch()
				if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
					return fmt.Errorf("failed to delete stale code marker: %w", err)
				}

				if err := batch.Write(); err != nil {
					return fmt.Errorf("failed to write batch for stale code marker: %w", err)
				}
				// Release in-flight ownership since no network fetch is needed.
				c.inFlight.Delete(codeHash)
				continue
			}

			codeHashes = append(codeHashes, codeHash)
			// Try to batch up to [codeHashesPerReq] code hashes into a single request when more work remains.
			if len(codeHashes) < c.codeHashesPerReq {
				continue
			}
			if err := c.fulfillCodeRequest(ctx, codeHashes); err != nil {
				return err
			}

			// Reset the codeHashes array
			codeHashes = codeHashes[:0]
		}
	}
}

// fulfillCodeRequest sends a request for codeHashes, writes the result to the
// database, and marks the work as complete. Returns an error if one is
// encountered, signaling the worker thread to terminate.
func (c *Syncer) fulfillCodeRequest(ctx context.Context, codeHashes []common.Hash) error {
	defer c.releaseInFlight(codeHashes)

	codeByteSlices, err := c.client.GetCode(ctx, codeHashes)
	if err != nil {
		return err
	}

	batch := c.db.NewBatch()
	for i, codeHash := range codeHashes {
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
		rawdb.WriteCode(batch, codeHash, codeByteSlices[i])
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch for fulfilled code requests: %w", err)
	}
	return nil
}
