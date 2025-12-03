// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/blocksync"
	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/statesync"
)

// BlocksToFetch is the number of the block parents the state syncs to.
// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
const BlocksToFetch = 256

var (
	errSkipSync         = fmt.Errorf("skip sync")
	stateSyncSummaryKey = []byte("stateSyncSummary")
)

// BlockAcceptor provides a mechanism to update the last accepted block ID during state synchronization.
// This interface is used by the state sync process to ensure the blockchain state
// is properly updated when new blocks are synchronized from the network.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// SyncStrategy defines how state sync is executed.
// Implementations handle the sync lifecycle differently based on sync mode.
type SyncStrategy interface {
	// Start begins the sync process and blocks until completion or error.
	Start(ctx context.Context) error
}

type ClientConfig struct {
	Chain      *eth.Ethereum
	State      *chain.State
	ChainDB    ethdb.Database
	Acceptor   BlockAcceptor
	VerDB      *versiondb.Database
	MetadataDB database.Database

	// Extension points.
	Parser message.SyncableParser

	// Extender is an optional extension point for the state sync process, and can be nil.
	Extender      syncpkg.Extender
	Client        syncclient.Client
	StateSyncDone chan struct{}

	// Specifies the number of blocks behind the latest state summary that the chain must be
	// in order to prefer performing state sync over falling back to the normal bootstrapping
	// algorithm.
	MinBlocks          uint64
	LastAcceptedHeight uint64
	RequestSize        uint16 // number of key/value pairs to ask peers for per request
	Enabled            bool
	SkipResume         bool
}

type client struct {
	config           *ClientConfig
	resumableSummary message.Syncable
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	err              error
}

func NewClient(config *ClientConfig) Client {
	return &client{
		config: config,
	}
}

type Client interface {
	// Methods that implement the client side of [block.StateSyncableVM].
	StateSyncEnabled(context.Context) (bool, error)
	GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error)
	ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error)

	// Additional methods required by the evm package.
	ClearOngoingSummary() error
	Shutdown() error
	Error() error
}

// StateSyncEnabled returns [client.enabled], which is set in the chain's config file.
func (c *client) StateSyncEnabled(context.Context) (bool, error) {
	return c.config.Enabled, nil
}

// GetOngoingSyncStateSummary returns a state summary that was previously started
// and not finished, and sets [resumableSummary] if one was found.
// Returns [database.ErrNotFound] if no ongoing summary is found or if [client.skipResume] is true.
func (c *client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	if c.config.SkipResume {
		return nil, database.ErrNotFound
	}

	summaryBytes, err := c.config.MetadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := c.config.Parser.Parse(summaryBytes, c.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	c.resumableSummary = summary
	return summary, nil
}

// ClearOngoingSummary clears any marker of an ongoing state sync summary
func (c *client) ClearOngoingSummary() error {
	if err := c.config.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := c.config.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// ParseStateSummary parses [summaryBytes] to [commonEng.Summary]
func (c *client) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return c.config.Parser.Parse(summaryBytes, c.acceptSyncSummary)
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (c *client) acceptSyncSummary(summary message.Syncable) (block.StateSyncMode, error) {
	if err := c.prepareForSync(summary); err != nil {
		if errors.Is(err, errSkipSync) {
			return block.StateSyncSkipped, nil
		}
		return block.StateSyncSkipped, err
	}

	registry, err := c.newSyncerRegistry(summary)
	if err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to create syncer registry: %w", err)
	}

	finalizer := newFinalizer(
		c.config.Chain,
		c.config.State,
		c.config.Acceptor,
		c.config.VerDB,
		c.config.MetadataDB,
		c.config.Extender,
		c.config.LastAcceptedHeight,
	)

	strategy := newStaticStrategy(registry, finalizer, summary)

	return c.startAsync(strategy), nil
}

// prepareForSync handles resume check and snapshot wipe before sync starts.
func (c *client) prepareForSync(summary message.Syncable) error {
	isResume := c.resumableSummary != nil &&
		summary.GetBlockHash() == c.resumableSummary.GetBlockHash()
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		// (Also ensures we don't sync to a height prior to local state.)
		if c.config.LastAcceptedHeight+c.config.MinBlocks > summary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", c.config.LastAcceptedHeight,
				"syncableHeight", summary.Height(),
			)
			return errSkipSync
		}

		// Wipe the snapshot completely if we are not resuming from an existing sync, so that we do not
		// use a corrupted snapshot.
		// Note: this assumes that when the node is started with state sync disabled, the in-progress state
		// sync marker will be wiped, so we do not accidentally resume progress from an incorrect version
		// of the snapshot. (if switching between versions that come before this change and back this could
		// lead to the snapshot not being cleaned up correctly)
		<-snapshot.WipeSnapshot(c.config.ChainDB, true)
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		snapshot.ResetSnapshotGeneration(c.config.ChainDB)
	}

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := c.config.MetadataDB.Put(stateSyncSummaryKey, summary.Bytes()); err != nil {
		return fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := c.config.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db: %w", err)
	}

	return nil
}

// startAsync launches the sync strategy in a background goroutine.
func (c *client) startAsync(strategy SyncStrategy) block.StateSyncMode {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()

		if err := strategy.Start(ctx); err != nil {
			c.err = err
		}
		// notify engine regardless of whether err == nil,
		// this error will be propagated to the engine when it calls
		// vm.SetState(snow.Bootstrapping)
		log.Info("state sync completed, notifying engine", "err", c.err)
		close(c.config.StateSyncDone)
	}()

	log.Info("state sync started", "mode", block.StateSyncStatic)
	return block.StateSyncStatic
}

func (c *client) Shutdown() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait() // wait for the background goroutine to exit
	return nil
}

// Error returns a non-nil error if one occurred during the sync.
func (c *client) Error() error { return c.err }

// newSyncerRegistry creates a registry with all required syncers for the given summary.
func (c *client) newSyncerRegistry(summary message.Syncable) (*SyncerRegistry, error) {
	registry := NewSyncerRegistry()

	blockSyncer, err := blocksync.NewSyncer(
		c.config.Client, c.config.ChainDB,
		summary.GetBlockHash(), summary.Height(),
		BlocksToFetch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create block syncer: %w", err)
	}

	codeQueue, err := statesync.NewCodeQueue(c.config.ChainDB, c.config.StateSyncDone)
	if err != nil {
		return nil, fmt.Errorf("failed to create code queue: %w", err)
	}

	codeSyncer, err := statesync.NewCodeSyncer(c.config.Client, c.config.ChainDB, codeQueue.CodeHashes())
	if err != nil {
		return nil, fmt.Errorf("failed to create code syncer: %w", err)
	}

	stateSyncer, err := statesync.NewSyncer(
		c.config.Client, c.config.ChainDB,
		summary.GetBlockRoot(),
		codeQueue, c.config.RequestSize,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM state syncer: %w", err)
	}

	syncers := []syncpkg.Syncer{blockSyncer, codeSyncer, stateSyncer}

	if c.config.Extender != nil {
		atomicSyncer, err := c.config.Extender.CreateSyncer(c.config.Client, c.config.VerDB, summary)
		if err != nil {
			return nil, fmt.Errorf("failed to create atomic syncer: %w", err)
		}
		syncers = append(syncers, atomicSyncer)
	}

	for _, s := range syncers {
		if err := registry.Register(s); err != nil {
			return nil, fmt.Errorf("failed to register %s syncer: %w", s.Name(), err)
		}
	}

	return registry, nil
}
