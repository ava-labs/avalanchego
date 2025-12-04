// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"

	statesyncclient "github.com/ava-labs/subnet-evm/sync/client"
)

const (
	DefaultMaxOutstandingCodeHashes = 5000
	DefaultNumCodeFetchingWorkers   = 5
)

var errFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")

// CodeSyncerConfig defines the configuration of the code syncer
type CodeSyncerConfig struct {
	// Maximum number of outstanding code hashes in the queue before the code syncer should block.
	MaxOutstandingCodeHashes int
	// Number of worker threads to fetch code from the network
	NumCodeFetchingWorkers int

	// Client for fetching code from the network
	Client statesyncclient.Client

	// Database for the code syncer to use.
	DB ethdb.Database
}

// codeSyncer syncs code bytes from the network in a separate thread.
// Tracks outstanding requests in the DB, so that it will still fulfill them if interrupted.
type codeSyncer struct {
	lock sync.Mutex

	CodeSyncerConfig

	outstandingCodeHashes set.Set[ids.ID]  // Set of code hashes that we need to fetch from the network.
	codeHashes            chan common.Hash // Channel of incoming code hash requests

	// Used to set terminal error or pass nil to [errChan] if successful.
	errOnce sync.Once
	errChan chan error

	// Passed in details from the context used to start the codeSyncer
	cancel context.CancelFunc
	done   <-chan struct{}
}

// newCodeSyncer returns a code syncer that will sync code bytes from the network in a separate thread.
func newCodeSyncer(config CodeSyncerConfig) *codeSyncer {
	return &codeSyncer{
		CodeSyncerConfig:      config,
		codeHashes:            make(chan common.Hash, config.MaxOutstandingCodeHashes),
		outstandingCodeHashes: set.NewSet[ids.ID](0),
		errChan:               make(chan error, 1),
	}
}

// start the worker thread and populate the code hashes queue with active work.
// blocks until all outstanding code requests from a previous sync have been
// queued for fetching (or ctx is cancelled).
func (c *codeSyncer) start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = ctx.Done()
	wg := sync.WaitGroup{}

	// Start [numCodeFetchingWorkers] threads to fetch code from the network.
	for i := 0; i < c.NumCodeFetchingWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := c.work(ctx); err != nil {
				c.setError(err)
			}
		}()
	}

	err := c.addCodeToFetchFromDBToQueue()
	if err != nil {
		c.setError(err)
	}

	// Wait for all the worker threads to complete before signalling success via setError(nil).
	// Note: if any of the worker threads errored already, setError will be a no-op here.
	go func() {
		wg.Wait()
		c.setError(nil)
	}()
}

// Clean out any codeToFetch markers from the database that are no longer needed and
// add any outstanding markers to the queue.
func (c *codeSyncer) addCodeToFetchFromDBToQueue() error {
	it := customrawdb.NewCodeToFetchIterator(c.DB)
	defer it.Release()

	batch := c.DB.NewBatch()
	codeHashes := make([]common.Hash, 0)
	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		// If we already have the codeHash, delete the marker from the database and continue
		if rawdb.HasCode(c.DB, codeHash) {
			customrawdb.DeleteCodeToFetch(batch, codeHash)
			// Write the batch to disk if it has reached the ideal batch size.
			if batch.ValueSize() > ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					return fmt.Errorf("failed to write batch removing old code markers: %w", err)
				}
				batch.Reset()
			}
			continue
		}

		codeHashes = append(codeHashes, codeHash)
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("failed to iterate code entries to fetch: %w", err)
	}
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
	}
	return c.addCode(codeHashes)
}

// work fulfills any incoming requests from the producer channel by fetching code bytes from the network
// and fulfilling them by updating the database.
func (c *codeSyncer) work(ctx context.Context) error {
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

			codeHashes = append(codeHashes, codeHash)
			// Try to wait for at least [MaxCodeHashesPerRequest] code hashes to batch into a single request
			// if there's more work remaining.
			if len(codeHashes) < message.MaxCodeHashesPerRequest {
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
func (c *codeSyncer) fulfillCodeRequest(ctx context.Context, codeHashes []common.Hash) error {
	codeByteSlices, err := c.Client.GetCode(ctx, codeHashes)
	if err != nil {
		return err
	}

	// Hold the lock while modifying outstandingCodeHashes.
	c.lock.Lock()
	batch := c.DB.NewBatch()
	for i, codeHash := range codeHashes {
		customrawdb.DeleteCodeToFetch(batch, codeHash)
		c.outstandingCodeHashes.Remove(ids.ID(codeHash))
		rawdb.WriteCode(batch, codeHash, codeByteSlices[i])
	}
	c.lock.Unlock() // Release the lock before writing the batch

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch for fulfilled code requests: %w", err)
	}
	return nil
}

// addCode checks if [codeHashes] need to be fetched from the network and adds them to the queue if so.
// assumes that [codeHashes] are valid non-empty code hashes.
func (c *codeSyncer) addCode(codeHashes []common.Hash) error {
	batch := c.DB.NewBatch()

	c.lock.Lock()
	selectedCodeHashes := make([]common.Hash, 0, len(codeHashes))
	for _, codeHash := range codeHashes {
		// Add the code hash to the queue if it's not already on the queue and we do not already have it
		// in the database.
		if !c.outstandingCodeHashes.Contains(ids.ID(codeHash)) && !rawdb.HasCode(c.DB, codeHash) {
			selectedCodeHashes = append(selectedCodeHashes, codeHash)
			c.outstandingCodeHashes.Add(ids.ID(codeHash))
			customrawdb.AddCodeToFetch(batch, codeHash)
		}
	}
	c.lock.Unlock()

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers due to: %w", err)
	}
	return c.addHashesToQueue(selectedCodeHashes)
}

// notifyAccountTrieCompleted notifies the code syncer that there will be no more incoming
// code hashes from syncing the account trie, so it only needs to complete its outstanding
// work.
// Note: this allows the worker threads to exit and return a nil error.
func (c *codeSyncer) notifyAccountTrieCompleted() {
	close(c.codeHashes)
}

// addHashesToQueue adds [codeHashes] to the queue and blocks until it is able to do so.
// This should be called after all other operation to add code hashes to the queue has been completed.
func (c *codeSyncer) addHashesToQueue(codeHashes []common.Hash) error {
	for _, codeHash := range codeHashes {
		select {
		case c.codeHashes <- codeHash:
		case <-c.done:
			return errFailedToAddCodeHashesToQueue
		}
	}
	return nil
}

// setError sets the error to the first error that occurs and adds it to the error channel.
// If [err] is nil, setError indicates that codeSyncer has finished code syncing successfully.
func (c *codeSyncer) setError(err error) {
	c.errOnce.Do(func() {
		c.cancel()
		c.errChan <- err
	})
}

// Done returns an error channel to indicate the return status of code syncing.
func (c *codeSyncer) Done() <-chan error { return c.errChan }
