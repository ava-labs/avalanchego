// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
)

var ErrFailedToFetchLeafs = errors.New("failed to fetch leafs")

// LeafSyncTask represents a complete task to be completed by the leaf syncer.
// Note: each LeafSyncTask is processed on its own goroutine and there will
// not be concurrent calls to the callback methods. Implementations should return
// the same value for Root, Account, Start, and NodeType throughout the sync.
// The value returned by End can change between calls to OnLeafs.
type LeafSyncTask interface {
	Root() common.Hash                                      // Root of the trie to sync
	Account() common.Hash                                   // Account hash of the trie to sync (only applicable to storage tries)
	Start() []byte                                          // Starting key to request new leaves
	End() []byte                                            // End key to request new leaves
	NodeType() message.NodeType                             // Specifies the message type (atomic/state trie) for the leaf syncer to send
	OnStart() (bool, error)                                 // Callback when tasks begins, returns true if work can be skipped
	OnLeafs(ctx context.Context, keys, vals [][]byte) error // Callback when new leaves are received from the network
	OnFinish(ctx context.Context) error                     // Callback when there are no more leaves in the trie to sync or when we reach End()
}

type LeafSyncerConfig struct {
	RequestSize uint16 // Number of leafs to request from a peer at a time
	NumWorkers  int    // Number of workers to process leaf sync tasks
}

type CallbackLeafSyncer struct {
	config *LeafSyncerConfig
	client LeafClient
	tasks  <-chan LeafSyncTask
}

type LeafClient interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error
	// Note: this verifies the response including the range proofs.
	GetLeafs(context.Context, message.LeafsRequest) (message.LeafsResponse, error)
}

// NewCallbackLeafSyncer creates a new syncer object to perform leaf sync of tries.
func NewCallbackLeafSyncer(client LeafClient, tasks <-chan LeafSyncTask, config *LeafSyncerConfig) *CallbackLeafSyncer {
	return &CallbackLeafSyncer{
		config: config,
		client: client,
		tasks:  tasks,
	}
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

		leafsResponse, err := c.client.GetLeafs(ctx, message.LeafsRequest{
			Root:     root,
			Account:  task.Account(),
			Start:    start,
			Limit:    c.config.RequestSize,
			NodeType: task.NodeType(),
		})
		if err != nil {
			return fmt.Errorf("%w: %w", ErrFailedToFetchLeafs, err)
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

		if err := task.OnLeafs(ctx, leafsResponse.Keys, leafsResponse.Vals); err != nil {
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
func (c *CallbackLeafSyncer) Sync(ctx context.Context) error {
	// Start the worker threads with the desired context.
	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < c.config.NumWorkers; i++ {
		eg.Go(func() error {
			return c.workerLoop(egCtx)
		})
	}

	return eg.Wait()
}
