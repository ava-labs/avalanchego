// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"

	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/ava-labs/libevm/log"
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

	for _, task := range r.syncers {
		log.Info("starting syncer", "name", task.name, "summary", client.summary)

		// Start syncer.
		if err := task.syncer.Start(ctx); err != nil {
			log.Info("failed to start", "name", task.name, "summary", client.summary, "err", err)
			return fmt.Errorf("failed to start %s: %w", task.name, err)
		}

		// Wait for completion.
		err := task.syncer.Wait(ctx)

		// Log completion immediately.
		if err != nil {
			log.Error("failed to complete", "name", task.name, "summary", client.summary, "err", err)
			return fmt.Errorf("%s failed: %w", task.name, err)
		} else {
			log.Info("completed successfully", "name", task.name, "summary", client.summary)
		}
	}

	return nil
}
