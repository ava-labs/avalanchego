// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/utils"
)

var (
	errFailedToFetchLeafs = errors.New("failed to fetch leafs")
)

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
	NodeType() message.NodeType         // Specifies the message type (atomic/state trie) for the leaf syncer to send
	OnStart() (bool, error)             // Callback when tasks begins, returns true if work can be skipped
	OnLeafs(keys, vals [][]byte) error  // Callback when new leaves are received from the network
	OnFinish(ctx context.Context) error // Callback when there are no more leaves in the trie to sync or when we reach End()
}

type CallbackLeafSyncer struct {
	client      LeafClient
	done        chan error
	tasks       <-chan LeafSyncTask
	requestSize uint16
}

type LeafClient interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error
	// Note: this verifies the response including the range proofs.
	GetLeafs(context.Context, message.LeafsRequest) (message.LeafsResponse, error)
}

// NewCallbackLeafSyncer creates a new syncer object to perform leaf sync of tries.
func NewCallbackLeafSyncer(client LeafClient, tasks <-chan LeafSyncTask, requestSize uint16) *CallbackLeafSyncer {
	return &CallbackLeafSyncer{
		client:      client,
		done:        make(chan error),
		tasks:       tasks,
		requestSize: requestSize,
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
			Limit:    c.requestSize,
			NodeType: task.NodeType(),
		})
		if err != nil {
			return fmt.Errorf("%s: %w", errFailedToFetchLeafs, err)
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
			return fmt.Errorf("found no keys in a response with more set to true")
		}
		// Update start to be one bit past the last returned key for the next request.
		// Note: since more was true, this cannot cause an overflow.
		start = leafsResponse.Keys[len(leafsResponse.Keys)-1]
		utils.IncrOne(start)
	}
}

// Start launches [numThreads] worker goroutines to process LeafSyncTasks from [c.tasks].
// onFailure is called if the sync completes with an error.
func (c *CallbackLeafSyncer) Start(ctx context.Context, numThreads int, onFailure func(error) error) {
	// Start the worker threads with the desired context.
	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < numThreads; i++ {
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
