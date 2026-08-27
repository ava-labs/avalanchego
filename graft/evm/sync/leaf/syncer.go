// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leaf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/libevm/common"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

var (
	ErrFailedToFetchLeafs = errors.New("failed to fetch leafs")
	ErrMoreWithoutKeys    = errors.New("more leaves reported but none returned")
)

// Fetcher reads a range of leaves from the network, already proven against the
// requested root. An error is terminal, and ctx bounds how long it re-requests.
type Fetcher interface {
	// FetchLeaves returns the leaves in ascending key order and reports whether
	// the trie holds more past them.
	FetchLeaves(ctx context.Context, req hashdb.LeafRange) (hashdb.Leaves, bool, error)
}

// SyncTask represents a complete task to be completed by the leaf syncer.
// Note: each SyncTask is processed on its own goroutine and there will
// not be concurrent calls to the callback methods. Implementations should return
// the same value for Root, Account, and Start throughout the sync.
// The value returned by End can change between calls to OnLeafs.
type SyncTask interface {
	Root() common.Hash                                      // Root of the trie to sync
	Account() common.Hash                                   // Account hash of the trie to sync (only applicable to storage tries)
	Start() []byte                                          // Starting key to request new leaves
	End() []byte                                            // End key to request new leaves
	OnStart() (bool, error)                                 // Callback when tasks begins, returns true if work can be skipped
	OnLeafs(ctx context.Context, keys, vals [][]byte) error // Callback when new leaves are received from the network
	OnFinish(ctx context.Context) error                     // Callback when there are no more leaves in the trie to sync or when we reach End()
}

type SyncerConfig struct {
	RequestSize uint16 // Number of leafs to request from a peer at a time
	NumWorkers  int    // Number of workers to process leaf sync tasks
}

type CallbackSyncer struct {
	config  *SyncerConfig
	fetcher Fetcher
	tasks   <-chan SyncTask
}

// NewCallbackSyncer creates a new syncer object to perform leaf sync of tries.
func NewCallbackSyncer(fetcher Fetcher, tasks <-chan SyncTask, config *SyncerConfig) *CallbackSyncer {
	return &CallbackSyncer{
		config:  config,
		fetcher: fetcher,
		tasks:   tasks,
	}
}

// workerLoop reads from [c.tasks] and calls [c.syncTask] until [ctx] is finished
// or [c.tasks] is closed.
func (c *CallbackSyncer) workerLoop(ctx context.Context) error {
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
func (c *CallbackSyncer) syncTask(ctx context.Context, task SyncTask) error {
	var (
		root  = task.Root()
		start = task.Start()
	)

	// LeafRange.Account is nil for the account trie, non-nil for a storage trie.
	var account *common.Hash
	if a := task.Account(); a != (common.Hash{}) {
		account = &a
	}

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

		leaves, more, err := c.fetcher.FetchLeaves(ctx, hashdb.LeafRange{
			Root:    root,
			Account: account,
			Start:   start,
			Limit:   c.config.RequestSize,
		})
		if err != nil {
			return fmt.Errorf("%w: %w", ErrFailedToFetchLeafs, err)
		}

		// The request carries no end key, so bound the response here. The common
		// case is nothing to cut, checked in O(1) before paying for the search.
		done := false
		if end := task.End(); end != nil && len(leaves.Keys) > 0 {
			if last := leaves.Keys[len(leaves.Keys)-1]; bytes.Compare(last, end) > 0 {
				n := sort.Search(len(leaves.Keys), func(i int) bool {
					return bytes.Compare(leaves.Keys[i], end) > 0
				})
				leaves.Keys, leaves.Vals = leaves.Keys[:n], leaves.Vals[:n]
				done = true
			}
		}

		// The last key is copied before [OnLeafs], which may retain and mutate it.
		var lastKey []byte
		if n := len(leaves.Keys); n > 0 {
			lastKey = common.CopyBytes(leaves.Keys[n-1])
		}

		if err := task.OnLeafs(ctx, leaves.Keys, leaves.Vals); err != nil {
			return err
		}

		// If we have completed syncing this task, invoke [OnFinish] and mark the task
		// as complete.
		if done || !more {
			return task.OnFinish(ctx)
		}

		if len(leaves.Keys) == 0 {
			return ErrMoreWithoutKeys
		}
		// Update start to be one bit past the last returned key for the next request.
		// Note: since more was true, this cannot cause an overflow.
		start = lastKey
		utils.IncrOne(start)
	}
}

// Sync launches [numWorkers] worker goroutines to process LeafSyncTasks from [c.tasks].
func (c *CallbackSyncer) Sync(ctx context.Context) error {
	// Start the worker threads with the desired context.
	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < c.config.NumWorkers; i++ {
		eg.Go(func() error {
			return c.workerLoop(egCtx)
		})
	}

	return eg.Wait()
}
