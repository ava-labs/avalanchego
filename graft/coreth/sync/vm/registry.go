// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/log"
	"golang.org/x/sync/errgroup"

	synccommon "github.com/ava-labs/coreth/sync"
)

// SyncerTask represents a single syncer with its name for identification.
type SyncerTask struct {
	name   string
	syncer synccommon.Syncer
}

// SyncerRegistry manages a collection of syncers for sequential execution.
type SyncerRegistry struct {
	syncers         []SyncerTask
	registeredNames map[string]bool // Track registered names to prevent duplicates.
}

// NewSyncerRegistry creates a new empty syncer registry.
func NewSyncerRegistry() *SyncerRegistry {
	return &SyncerRegistry{
		registeredNames: make(map[string]bool),
	}
}

// Register adds a syncer to the registry.
// Returns an error if a syncer with the same name is already registered.
func (r *SyncerRegistry) Register(name string, syncer synccommon.Syncer) error {
	if r.registeredNames[name] {
		return fmt.Errorf("syncer with name '%s' is already registered", name)
	}

	r.registeredNames[name] = true
	r.syncers = append(r.syncers, SyncerTask{name, syncer})

	return nil
}

// RunSyncerTasks executes all registered syncers.
func (r *SyncerRegistry) RunSyncerTasks(ctx context.Context, client *client) error {
	if len(r.syncers) == 0 {
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)

	for _, task := range r.syncers {
		g.Go(func() error {
			log.Info("starting syncer", "name", task.name, "summary", client.summary)
			if err := task.syncer.Sync(ctx); err != nil {
				log.Error("failed syncing", "name", task.name, "summary", client.summary, "err", err)
				return fmt.Errorf("%s failed: %w", task.name, err)
			}
			log.Info("completed successfully", "name", task.name, "summary", client.summary)

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	log.Info("all syncers completed successfully", "count", len(r.syncers), "summary", client.summary)

	return nil
}
