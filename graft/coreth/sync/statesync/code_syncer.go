// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	statesyncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

const defaultNumCodeFetchingWorkers = 5

var _ syncpkg.Syncer = (*CodeSyncer)(nil)

// CodeSyncer syncs code bytes from the network in a separate thread.
// It consumes code hashes from a queue and persists code into the DB.
// Outstanding requests are tracked via durable "to-fetch" markers in the DB for recovery.
// The syncer performs in-flight deduplication and skips locally-present code before issuing requests.
type CodeSyncer struct {
	db     ethdb.Database
	client statesyncclient.Client
	// Channel of incoming code hash requests provided by the fetcher.
	codeHashes <-chan common.Hash

	// Config options.
	numWorkers       int
	codeHashesPerReq int // best-effort target size - final batch may be smaller

	// inFlight tracks code hashes currently being processed to dedupe work
	// across workers and across repeated queue submissions.
	inFlight sync.Map // key: common.Hash, value: struct{}
}

// codeSyncerConfig carries construction-time options for code syncer.
type codeSyncerConfig struct {
	numWorkers       int
	codeHashesPerReq int
}

// CodeSyncerOption configures CodeSyncer at construction time.
type CodeSyncerOption = options.Option[codeSyncerConfig]

// WithNumWorkers overrides the number of concurrent workers.
func WithNumWorkers(n int) CodeSyncerOption {
	return options.Func[codeSyncerConfig](func(c *codeSyncerConfig) {
		if n > 0 {
			c.numWorkers = n
		}
	})
}

// WithCodeHashesPerRequest sets the best-effort target batch size per request.
// The final batch may contain fewer than the configured number if insufficient
// hashes remain when the channel is closed.
func WithCodeHashesPerRequest(n int) CodeSyncerOption {
	return options.Func[codeSyncerConfig](func(c *codeSyncerConfig) {
		if n > 0 {
			c.codeHashesPerReq = n
		}
	})
}

// NewCodeSyncer allows external packages (e.g., registry wiring) to create a code syncer
// that consumes hashes from a provided fetcher queue.
func NewCodeSyncer(client statesyncclient.Client, db ethdb.Database, codeHashes <-chan common.Hash, opts ...CodeSyncerOption) (*CodeSyncer, error) {
	cfg := codeSyncerConfig{
		numWorkers:       defaultNumCodeFetchingWorkers,
		codeHashesPerReq: message.MaxCodeHashesPerRequest,
	}
	options.ApplyTo(&cfg, opts...)

	return &CodeSyncer{
		db:               db,
		client:           client,
		codeHashes:       codeHashes,
		numWorkers:       cfg.numWorkers,
		codeHashesPerReq: cfg.codeHashesPerReq,
	}, nil
}

// Name returns the human-readable name for this sync task.
func (*CodeSyncer) Name() string {
	return "Code Syncer"
}

// ID returns the stable identifier for this sync task.
func (*CodeSyncer) ID() string {
	return "state_code_sync"
}

// Sync starts the worker thread and populates the code hashes queue with active work.
// Blocks until all outstanding code requests from a previous sync have been
// fetched and the code channel has been closed, or the context is cancelled.
func (c *CodeSyncer) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)

	// Start NumCodeFetchingWorkers threads to fetch code from the network.
	for range c.numWorkers {
		eg.Go(func() error { return c.work(egCtx) })
	}

	return eg.Wait()
}

func (*CodeSyncer) UpdateTarget(_ message.Syncable) error {
	return nil
}

func (*CodeSyncer) Finalize(_ context.Context) error {
	return nil
}

// work fulfills any incoming requests from the producer channel by fetching code bytes from the network
// and fulfilling them by updating the database.
func (c *CodeSyncer) work(ctx context.Context) error {
	codeHashes := make([]common.Hash, 0, message.MaxCodeHashesPerRequest)

	for {
		select {
		case <-ctx.Done(): // If ctx is done, set the error to the ctx error since work has been cancelled.
			return ctx.Err()
		case codeHash, ok := <-c.codeHashes:
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
				customrawdb.DeleteCodeToFetch(batch, codeHash)

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

// fulfillCodeRequest sends a request for [codeHashes], writes the result to the database, and
// marks the work as complete.
// codeHashes should not be empty or contain duplicate hashes.
// Returns an error if one is encountered, signaling the worker thread to terminate.
func (c *CodeSyncer) fulfillCodeRequest(ctx context.Context, codeHashes []common.Hash) error {
	codeByteSlices, err := c.client.GetCode(ctx, codeHashes)
	if err != nil {
		return err
	}

	batch := c.db.NewBatch()
	for i, codeHash := range codeHashes {
		customrawdb.DeleteCodeToFetch(batch, codeHash)
		rawdb.WriteCode(batch, codeHash, codeByteSlices[i])
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch for fulfilled code requests: %w", err)
	}
	// After successfully committing to the database, release in-flight ownership
	// so that subsequent work for these hashes can be considered again if needed.
	for _, codeHash := range codeHashes {
		c.inFlight.Delete(codeHash)
	}
	return nil
}
