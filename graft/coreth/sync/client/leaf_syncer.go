// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// pendingRequest represents a leaf request that has been issued and is awaiting response.
// The issuer goroutine populates response/err, then sends the request to the pipeline channel.
// The processor goroutine receives from the pipeline, ensuring happens-before relationship.
// This guarantees the processor sees all writes made before the channel send.
type pendingRequest struct {
	request  message.LeafsRequest
	response message.LeafsResponse
	err      error
}

// syncTask performs [task], requesting the leaves of the trie corresponding to [task.Root]
// starting at [task.Start] and invoking the callbacks as necessary.
// Uses request pipelining to overlap network I/O with processing.
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

	// Pipeline depth: keep 2 requests in flight to overlap network I/O with processing
	const pipelineDepth = 2
	pipeline := make(chan *pendingRequest, pipelineDepth)
	eg, egCtx := errgroup.WithContext(ctx)

	// Request issuer goroutine - keeps pipeline filled
	eg.Go(func() error {
		defer close(pipeline)
		currentStart := start

		for {
			// Check if context is done before issuing next request
			if egCtx.Err() != nil {
				return nil
			}

			req := &pendingRequest{
				request: message.LeafsRequest{
					Root:     root,
					Account:  task.Account(),
					Start:    currentStart,
					Limit:    c.config.RequestSize,
					NodeType: task.NodeType(),
				},
			}

			// Issue request and populate response
			response, err := c.client.GetLeafs(egCtx, req.request)

			// CRITICAL: Populate req completely BEFORE sending to pipeline.
			// Channel send/receive provides happens-before guarantee, ensuring
			// the processor goroutine sees these writes after receiving from channel.
			req.response = response
			req.err = err

			// Send to pipeline - will block if pipeline is full (backpressure)
			select {
			case pipeline <- req:
			case <-egCtx.Done():
				return nil
			}

			// If this request completed the sync or errored, stop issuing
			if err != nil || !response.More || len(response.Keys) == 0 {
				return nil
			}

			// Calculate next start position
			currentStart = response.Keys[len(response.Keys)-1]
			utils.IncrOne(currentStart)
		}
	})

	// Response processor - main goroutine consumes from pipeline
	eg.Go(func() error {
		for req := range pipeline {
			if req.err != nil {
				return fmt.Errorf("%w: %w", ErrFailedToFetchLeafs, req.err)
			}

			leafsResponse := req.response

			// resize [leafsResponse.Keys] and [leafsResponse.Vals] in case
			// the response includes any keys past [End()].
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

			if err := task.OnLeafs(egCtx, leafsResponse.Keys, leafsResponse.Vals); err != nil {
				return err
			}

			// If we have completed syncing this task, invoke [OnFinish] and mark complete
			if done || !leafsResponse.More {
				return task.OnFinish(egCtx)
			}

			if len(leafsResponse.Keys) == 0 {
				return errors.New("found no keys in a response with more set to true")
			}
		}
		return nil
	})

	return eg.Wait()
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
