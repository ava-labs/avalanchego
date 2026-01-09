// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/message"
)

var (
	// Thread-safe random source for jitter calculation
	// Note: Go 1.20+ global rand is auto-seeded and has internal locking,
	// but we use a dedicated source to avoid contention with other code
	leafRngMu sync.Mutex
	leafRng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

const (
	// Retry configuration for leaf requests
	leafRequestInitialDelay = 50 * time.Millisecond
	leafRequestMaxDelay     = 8 * time.Second
	leafRequestMaxRetries   = 15
	leafRequestJitterFactor = 0.25 // 25% jitter
)

var errFailedToFetchLeafs = errors.New("failed to fetch leafs")

// LeafSyncTask represents a complete task to be completed by the leaf syncer.
// Note: each LeafSyncTask is processed on its own goroutine and there will
// not be concurrent calls to the callback methods. Implementations should return
// the same value for Root, Account, Start, and NodeType throughout the sync.
// The value returned by End can change between calls to OnLeafs.
type LeafSyncTask interface {
	Root() common.Hash                  // Root of the trie to sync
	Account() common.Hash               // Account hash of the trie to sync (only applicable to storage tries)
	Start() []byte                      // Starting key to request new leaves
	End() []byte                        // End key to request new leaves
	OnStart() (bool, error)             // Callback when tasks begins, returns true if work can be skipped
	OnLeafs(keys, vals [][]byte) error  // Callback when new leaves are received from the network
	OnFinish(ctx context.Context) error // Callback when there are no more leaves in the trie to sync or when we reach End()
}

type CallbackLeafSyncer struct {
	client           LeafClient
	done             chan error
	tasks            <-chan LeafSyncTask
	requestSize      uint16
	adaptiveSize     atomic.Uint32 // Current adaptive request size
	consecutiveFailures atomic.Uint32
}

type LeafClient interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error
	// Note: this verifies the response including the range proofs.
	GetLeafs(context.Context, message.LeafsRequest) (message.LeafsResponse, error)
}

// NewCallbackLeafSyncer creates a new syncer object to perform leaf sync of tries.
func NewCallbackLeafSyncer(client LeafClient, tasks <-chan LeafSyncTask, requestSize uint16) *CallbackLeafSyncer {
	syncer := &CallbackLeafSyncer{
		client:      client,
		done:        make(chan error),
		tasks:       tasks,
		requestSize: requestSize,
	}
	syncer.adaptiveSize.Store(uint32(requestSize))
	return syncer
}

// exponentialBackoffWithJitter calculates backoff with jitter to prevent thundering herd
func exponentialBackoffWithJitter(attempt int, initialDelay, maxDelay time.Duration, jitterFactor float64) time.Duration {
	// Validate inputs
	if attempt < 0 {
		attempt = 0
	}
	if initialDelay <= 0 {
		initialDelay = 50 * time.Millisecond // Safe default
	}
	if maxDelay <= 0 || maxDelay < initialDelay {
		maxDelay = 8 * time.Second // Safe default
	}
	if jitterFactor < 0 || jitterFactor > 1 {
		jitterFactor = 0.25 // Safe default
	}

	// Calculate exponential: initialDelay * 2^attempt (capped at 2^10)
	exp := attempt
	if exp > 10 {
		exp = 10
	}

	delay := initialDelay * time.Duration(1<<exp)
	if delay > maxDelay || delay < 0 { // Check for overflow
		delay = maxDelay
	}

	// Add jitter: delay * (1 +/- jitterFactor)
	if jitterFactor > 0 {
		jitterRange := float64(delay) * jitterFactor

		// Thread-safe random number generation
		leafRngMu.Lock()
		randVal := leafRng.Float64()
		leafRngMu.Unlock()

		jitter := (randVal*2 - 1) * jitterRange
		delay = time.Duration(float64(delay) + jitter)

		// Clamp to valid range [initialDelay, maxDelay]
		if delay < initialDelay {
			delay = initialDelay
		}
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return delay
}

// isLeafRequestFatalError determines if error is fatal or transient
func isLeafRequestFatalError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Fatal: context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Fatal: invalid range proof (data corruption)
	if strings.Contains(errStr, "invalid range proof") ||
		strings.Contains(errStr, "failed to verify range proof") {
		return true
	}

	// All other errors (network, timeouts, peers) are transient
	return false
}

// workerLoop reads from [c.tasks] and calls [c.syncTask] until [ctx] is finished
// or [c.tasks] is closed.
func (c *CallbackLeafSyncer) workerLoop(ctx context.Context) error {
	for {
		select {
		case task, more := <-c.tasks:
			if !more {
				return nil
			}
			if err := c.syncTask(ctx, task); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncTask performs [task], requesting the leaves of the trie corresponding to [task.Root]
// starting at [task.Start] and invoking the callbacks as necessary.
func (c *CallbackLeafSyncer) syncTask(ctx context.Context, task LeafSyncTask) error {
	var (
		root  = task.Root()
		start = task.Start()
	)

	if skip, err := task.OnStart(); err != nil {
		return err
	} else if skip {
		return nil
	}

	for {
		// If [ctx] has finished, return early.
		if err := ctx.Err(); err != nil {
			return err
		}

		// Use adaptive request size
		currentSize := uint16(c.adaptiveSize.Load())

		var (
			leafsResponse message.LeafsResponse
			err           error
			lastErr       error
		)

		// Retry loop with exponential backoff + jitter
		for attempt := 0; attempt < leafRequestMaxRetries; attempt++ {
			if attempt > 0 {
				// Apply exponential backoff with jitter
				backoff := exponentialBackoffWithJitter(
					attempt-1,
					leafRequestInitialDelay,
					leafRequestMaxDelay,
					leafRequestJitterFactor,
				)

				log.Debug("Retrying leaf request after backoff",
					"attempt", attempt,
					"backoff", backoff,
					"root", root,
					"currentSize", currentSize,
					"lastErr", lastErr)

				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			leafsResponse, err = c.client.GetLeafs(ctx, message.LeafsRequest{
				Root:     root,
				Account:  task.Account(),
				Start:    start,
				Limit:    currentSize,
				NodeType: message.StateTrieNode,
			})

			if err == nil {
				if attempt > 0 {
					log.Info("Leaf request succeeded after retries", "attempts", attempt+1, "root", root)
				}
				break // Success!
			}

			lastErr = err

			// Check if fatal
			if isLeafRequestFatalError(err) {
				return fmt.Errorf("fatal leaf request error: %w", err)
			}

			// Transient error - log appropriately based on whether we'll retry
			if attempt+1 < leafRequestMaxRetries {
				log.Warn("Leaf request failed, will retry",
					"attempt", attempt+1,
					"maxRetries", leafRequestMaxRetries,
					"err", err,
					"currentSize", currentSize)
			} else {
				log.Error("Leaf request failed on final attempt",
					"attempt", attempt+1,
					"maxRetries", leafRequestMaxRetries,
					"err", err,
					"currentSize", currentSize)
			}
		}

		if err != nil {
			// Exhausted retries - reduce request size for next task
			c.consecutiveFailures.Add(1)
			failures := c.consecutiveFailures.Load()
			if failures >= 3 && currentSize > 64 {
				newSize := currentSize / 2
				if newSize < 64 {
					newSize = 64
				}
				c.adaptiveSize.Store(uint32(newSize))
				log.Debug("Reducing adaptive request size due to failures",
					"oldSize", currentSize,
					"newSize", newSize,
					"failures", failures)
			}
			return fmt.Errorf("%w after %d retries: %w", errFailedToFetchLeafs, leafRequestMaxRetries, lastErr)
		}

		// On success, reset failure counter and consider increasing size
		c.consecutiveFailures.Store(0)
		if currentSize < c.requestSize {
			// Gradually increase back to configured max
			newSize := currentSize * 2
			if newSize > c.requestSize {
				newSize = c.requestSize
			}
			c.adaptiveSize.Store(uint32(newSize))
			log.Debug("Increasing adaptive request size after success",
				"oldSize", currentSize,
				"newSize", newSize)
		}

		// resize [leafsResponse.Keys] and [leafsResponse.Vals] in case
		// the response includes any keys past [End()].
		// Note: We truncate the response here as opposed to sending End
		// in the request, as [VerifyRangeProof] does not handle empty
		// responses correctly with a non-empty end key for the range.
		done := false
		if task.End() != nil && len(leafsResponse.Keys) > 0 {
			i := len(leafsResponse.Keys) - 1
			for ; i >= 0; i-- {
				if bytes.Compare(leafsResponse.Keys[i], task.End()) <= 0 {
					break
				}
				done = true
			}
			leafsResponse.Keys = leafsResponse.Keys[:i+1]
			leafsResponse.Vals = leafsResponse.Vals[:i+1]
		}

		if err := task.OnLeafs(leafsResponse.Keys, leafsResponse.Vals); err != nil {
			return err
		}

		// If we have completed syncing this task, invoke [OnFinish] and mark the task
		// as complete.
		if done || !leafsResponse.More {
			return task.OnFinish(ctx)
		}

		if len(leafsResponse.Keys) == 0 {
			return errors.New("found no keys in a response with more set to true")
		}
		// Update start to be one bit past the last returned key for the next request.
		// Note: since more was true, this cannot cause an overflow.
		start = leafsResponse.Keys[len(leafsResponse.Keys)-1]
		utils.IncrOne(start)
	}
}

// Start launches [numWorkers] worker goroutines to process LeafSyncTasks from [c.tasks].
// onFailure is called if the sync completes with an error.
func (c *CallbackLeafSyncer) Start(ctx context.Context, numWorkers int, onFailure func(error) error) {
	// Start the worker threads with the desired context.
	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < numWorkers; i++ {
		eg.Go(func() error {
			return c.workerLoop(egCtx)
		})
	}

	go func() {
		err := eg.Wait()
		if err != nil {
			if err := onFailure(err); err != nil {
				log.Error("error handling onFailure callback", "err", err)
			}
		}
		c.done <- err
		close(c.done)
	}()
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (c *CallbackLeafSyncer) Done() <-chan error { return c.done }
