// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	statesyncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

const (
	defaultNumCodeFetchingWorkers = 12  // Match leaf syncer workers for balanced I/O
	codeHashCheckBatchSize        = 64  // Batch size for HasCode() checks to reduce DB overhead
)

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

// work fulfills any incoming requests from the producer channel by fetching code bytes from the network
// and fulfilling them by updating the database.
// Uses batched HasCode() checks to reduce database overhead.
func (c *CodeSyncer) work(ctx context.Context) error {
	pendingHashes := make([]common.Hash, 0, codeHashCheckBatchSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case codeHash, ok := <-c.codeHashes:
			// If there are no more code hashes, process remaining batch and return
			if !ok {
				if len(pendingHashes) > 0 {
					return c.processBatch(ctx, pendingHashes)
				}
				return nil
			}

			// Deduplicate in-flight code hashes across workers first to avoid
			// racing repeated HasCode() checks for the same hash.
			if _, loaded := c.inFlight.LoadOrStore(codeHash, struct{}{}); loaded {
				continue
			}

			pendingHashes = append(pendingHashes, codeHash)

			// Process batch when full to amortize DB overhead
			if len(pendingHashes) >= codeHashCheckBatchSize {
				if err := c.processBatch(ctx, pendingHashes); err != nil {
					return err
				}
				pendingHashes = pendingHashes[:0]
			}
		}
	}
}

// processBatch checks which code hashes already exist locally, cleans up their markers,
// and fetches the missing ones from the network in a single batch operation.
func (c *CodeSyncer) processBatch(ctx context.Context, hashes []common.Hash) error {
	// Batch check which codes already exist to minimize DB overhead
	existingCodes := c.batchHasCode(hashes)

	// Separate existing codes (cleanup only) from missing codes (need fetch)
	toFetch := make([]common.Hash, 0, len(hashes))
	batch := c.db.NewBatch()

	for i, hash := range hashes {
		if existingCodes[i] {
			// Code already present - clean up stale marker
			if err := customrawdb.DeleteCodeToFetch(batch, hash); err != nil {
				return fmt.Errorf("failed to delete stale code marker: %w", err)
			}
			c.inFlight.Delete(hash)
		} else {
			// Code missing - needs network fetch
			toFetch = append(toFetch, hash)
		}
	}

	// Write cleanup batch if any markers were deleted
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write batch for stale code markers: %w", err)
		}
	}

	// Fetch missing codes from network
	if len(toFetch) > 0 {
		return c.fulfillCodeRequest(ctx, toFetch)
	}
	return nil
}

// batchHasCode checks multiple code hashes in parallel to reduce I/O latency.
// Parallelizes database reads across worker goroutines for better throughput.
// Returns a boolean slice where result[i] indicates whether hashes[i] exists.
func (c *CodeSyncer) batchHasCode(hashes []common.Hash) []bool {
	results := make([]bool, len(hashes))
	if len(hashes) == 0 {
		return results
	}

	// For small batches, sequential is faster due to goroutine overhead
	if len(hashes) <= 4 {
		for i, hash := range hashes {
			results[i] = rawdb.HasCode(c.db, hash)
		}
		return results
	}

	// Parallel checks for larger batches - improves I/O scheduling
	var wg sync.WaitGroup
	numWorkers := min(8, len(hashes)) // Cap workers to avoid excessive goroutines
	chunkSize := (len(hashes) + numWorkers - 1) / numWorkers

	for workerID := 0; workerID < numWorkers; workerID++ {
		start := workerID * chunkSize
		if start >= len(hashes) {
			break
		}
		end := min(start+chunkSize, len(hashes))

		wg.Add(1)
		go func(startIdx, endIdx int) {
			defer wg.Done()
			for i := startIdx; i < endIdx; i++ {
				results[i] = rawdb.HasCode(c.db, hashes[i])
			}
		}(start, end)
	}

	wg.Wait()
	return results
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
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
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
