// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
)

var errSyncerAlreadyRegistered = errors.New("syncer already registered")

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer syncpkg.Syncer
}

// SyncerRegistry manages a collection of syncers for sequential execution.
type SyncerRegistry struct {
	syncers         []SyncerTask
	registeredNames map[string]bool // Track registered IDs to prevent duplicates.
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	return &SyncerRegistry{
		registeredNames: make(map[string]bool),
	}
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered.
func (r *SyncerRegistry) Register(syncer syncpkg.Syncer) error {
	id := syncer.ID()
	if r.registeredNames[id] {
		return fmt.Errorf("%w with id '%s'", errSyncerAlreadyRegistered, id)
	}

	r.registeredNames[id] = true
	r.syncers = append(r.syncers, SyncerTask{syncer.Name(), syncer})

	return nil
}

// RunSyncerTasks executes all registered syncers synchronously.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, summary message.Syncable) error {
	// Early return if context is already canceled (e.g., during shutdown).
	if err := ctx.Err(); err != nil {
		return err
	}

	g := r.StartAsync(ctx, summary)

	if err := g.Wait(); err != nil {
		return err
	}

	log.Info("all syncers completed successfully", "count", len(r.syncers), "summary", summary.GetBlockHash().Hex())

	return nil
}

// StartAsync launches all registered syncers and returns an [errgroup.Group]
// whose Wait() completes when all syncers exit. The context returned will be
// cancelled when any syncer fails, propagating shutdown to the others.
func (r *SyncerRegistry) StartAsync(ctx context.Context, summary message.Syncable) *errgroup.Group {
	g, egCtx := errgroup.WithContext(ctx)

	if len(r.syncers) == 0 {
		return g
	}

	summaryBlockHashHex := summary.GetBlockHash().Hex()
	blockHeight := summary.Height()

	for _, task := range r.syncers {
		g.Go(func() error {
			log.Info("starting syncer", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)
			if err := task.syncer.Sync(egCtx); err != nil {
				// Context cancellation during shutdown is expected.
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					log.Info("syncer cancelled", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)
					return err
				}
				log.Error("failed syncing", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight, "err", err)
				return fmt.Errorf("%s failed: %w", task.name, err)
			}
			log.Info("completed successfully", "name", task.name, "summary", summaryBlockHashHex, "height", blockHeight)

			return nil
		})
	}

	return g
}
