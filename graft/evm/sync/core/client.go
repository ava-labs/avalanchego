// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	blocksync "github.com/ava-labs/avalanchego/graft/evm/sync/block"
	"github.com/ava-labs/avalanchego/graft/evm/sync/syncclient"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/graft/evm/vm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	evmtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

// BlocksToFetch is the number of the block parents the state syncs to.
// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
const BlocksToFetch = 256

var (
	errSkipSync            = errors.New("skip sync")
	errBlockNotFound       = errors.New("block not found in state")
	errInvalidBlockType    = errors.New("invalid block wrapper type")
	errBlockHashMismatch   = errors.New("block hash mismatch")
	errBlockHeightMismatch = errors.New("block height mismatch")
	errCommitCancelled     = errors.New("commit cancelled")
	errCommitMarkers       = errors.New("failed to commit VM markers")
	stateSyncSummaryKey    = []byte("stateSyncSummary")
)

// EthBlockWrapper can be implemented by a concrete block wrapper type to
// return *types.Block, which is needed to update chain pointers at the
// end of the sync operation.
type EthBlockWrapper interface {
	GetEthBlock() *evmtypes.Block
}

// ethBlockWrapper is a simple implementation of vm.BlockWrapper
// that wraps an Ethereum block for use with ProcessSyncedBlock.
type ethBlockWrapper struct {
	block *evmtypes.Block
}

func (e *ethBlockWrapper) GetEthBlock() *evmtypes.Block {
	return e.block
}

// BlockAcceptor provides a mechanism to update the last accepted block ID during state synchronization.
// This interface is used by the state sync process to ensure the blockchain state
// is properly updated when new blocks are synchronized from the network.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// Acceptor applies the results of state sync to the VM, preparing it for bootstrapping.
type Acceptor interface {
	AcceptSync(ctx context.Context, summary message.Syncable) error
}

// Executor defines how state sync is executed.
// Implementations handle the sync lifecycle differently based on sync mode.
type Executor interface {
	// Execute runs the sync process and blocks until completion or error.
	Execute(ctx context.Context, summary message.Syncable) error
}

// Extender is an interface that allows for extending the state sync process.
// It is used by VMs that need to add additional sync operations (e.g., Coreth's atomic trie sync).
type Extender interface {
	// CreateSyncer creates a syncer instance for the given client, database, and summary.
	CreateSyncer(client syncclient.LeafClient, verDB *versiondb.Database, summary message.Syncable) (types.Syncer, error)

	// OnFinishBeforeCommit is called before committing the sync results.
	OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error

	// OnFinishAfterCommit is called after committing the sync results.
	OnFinishAfterCommit(summaryHeight uint64) error
}

var _ Acceptor = (*client)(nil)

type ClientConfig struct {
	// VM Adapter - provides abstracted VM functionality
	VMAdapter vm.VMAdapter

	// VM-specific configuration
	ChainDB       ethdb.Database
	MetadataDB    database.Database
	StateSyncDone chan struct{}

	// Extension points.
	Parser message.SyncableParser

	// Extender is an optional extension point for the state sync process, and can be nil.
	Extender Extender

	// State sync client (already abstracted)
	Client syncclient.Client

	// SnapshotFactory creates a snapshot iterable for state sync.
	// This is required for EVM state syncing.
	SnapshotFactory evmstate.SnapshotFactory

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
	chainState, err := c.config.VMAdapter.GetChainState()
	if err != nil {
		return fmt.Errorf("failed to get chain state: %w", err)
	}
	if err := chainState.VerDB.Commit(); err != nil {
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

	executor := newStaticExecutor(registry, c)

	return c.startAsync(executor, summary), nil
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
		if err := c.config.VMAdapter.WipeSnapshot(); err != nil {
			return fmt.Errorf("failed to wipe snapshot: %w", err)
		}
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		if err := c.config.VMAdapter.ResetSnapshotGenerator(); err != nil {
			return fmt.Errorf("failed to reset snapshot generator: %w", err)
		}
	}

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := c.config.MetadataDB.Put(stateSyncSummaryKey, summary.Bytes()); err != nil {
		return fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	chainState, err := c.config.VMAdapter.GetChainState()
	if err != nil {
		return fmt.Errorf("failed to get chain state: %w", err)
	}
	if err := chainState.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db: %w", err)
	}

	return nil
}

// startAsync launches the sync executor in a background goroutine.
func (c *client) startAsync(executor Executor, summary message.Syncable) block.StateSyncMode {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()

		if err := executor.Execute(ctx, summary); err != nil {
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

// AcceptSync implements Acceptor. It resets the blockchain to the synced block,
// preparing it for execution, and updates disk and memory pointers so the VM
// is ready for bootstrapping. Also executes any shared memory operations from
// the atomic trie to shared memory.
func (c *client) AcceptSync(ctx context.Context, summary message.Syncable) error {
	// Get the synced block from the VM
	stateBlock, err := c.config.VMAdapter.GetBlock(ctx, ids.ID(summary.GetBlockHash()))
	if err != nil {
		return fmt.Errorf("%w: hash=%s", errBlockNotFound, summary.GetBlockHash())
	}

	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("%w: got %T, want *chain.BlockWrapper", errInvalidBlockType, stateBlock)
	}
	wrappedBlock := wrapper.Block

	evmBlockGetter, ok := wrappedBlock.(EthBlockWrapper)
	if !ok {
		return fmt.Errorf("%w: got %T, want EthBlockWrapper", errInvalidBlockType, wrappedBlock)
	}

	block := evmBlockGetter.GetEthBlock()

	if block.Hash() != summary.GetBlockHash() {
		return fmt.Errorf("%w: got %s, want %s", errBlockHashMismatch, block.Hash(), summary.GetBlockHash())
	}
	if block.NumberU64() != summary.Height() {
		return fmt.Errorf("%w: got %d, want %d", errBlockHeightMismatch, block.NumberU64(), summary.Height())
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errCommitCancelled, err)
	}

	// Process the synced block using the VM adapter.
	blockWrapper := &ethBlockWrapper{block: block}
	if err := c.config.VMAdapter.ProcessSyncedBlock(blockWrapper); err != nil {
		return err
	}

	// Set the last accepted block via the VM adapter.
	if err := c.config.VMAdapter.SetLastAcceptedBlock(wrappedBlock); err != nil {
		return err
	}

	if c.config.Extender != nil {
		if err := c.config.Extender.OnFinishBeforeCommit(c.config.LastAcceptedHeight, summary); err != nil {
			return err
		}
	}

	if err := c.commitMarkers(summary); err != nil {
		return fmt.Errorf("%w: height=%d, hash=%s: %w", errCommitMarkers, block.NumberU64(), block.Hash(), err)
	}

	if err := c.config.VMAdapter.UpdateChainPointers(summary); err != nil {
		return err
	}

	if c.config.Extender != nil {
		if err := c.config.Extender.OnFinishAfterCommit(block.NumberU64()); err != nil {
			return err
		}
	}

	return nil
}

// commitMarkers updates VM database markers atomically.
func (c *client) commitMarkers(summary message.Syncable) error {
	chainState, err := c.config.VMAdapter.GetChainState()
	if err != nil {
		return fmt.Errorf("failed to get chain state: %w", err)
	}

	if err := c.config.VMAdapter.UpdateChainPointers(summary); err != nil {
		return err
	}

	if err := chainState.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}

	return chainState.VerDB.Commit()
}

// newSyncerRegistry creates a registry with all required syncers for the given summary.
func (c *client) newSyncerRegistry(summary message.Syncable) (*SyncerRegistry, error) {
	chainState, err := c.config.VMAdapter.GetChainState()
	if err != nil {
		return nil, fmt.Errorf("failed to get chain state: %w", err)
	}

	registry := NewSyncerRegistry()

	blockSyncer, err := blocksync.NewSyncer(
		c.config.Client, chainState.ChainDB,
		summary.GetBlockHash(), summary.Height(),
		BlocksToFetch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create block syncer: %w", err)
	}

	codeQueue, err := code.NewQueue(chainState.ChainDB, c.config.StateSyncDone)
	if err != nil {
		return nil, fmt.Errorf("failed to create code queue: %w", err)
	}

	codeSyncer, err := code.NewSyncer(c.config.Client, chainState.ChainDB, codeQueue.CodeHashes())
	if err != nil {
		return nil, fmt.Errorf("failed to create code syncer: %w", err)
	}

	stateSyncer, err := evmstate.NewSyncer(
		c.config.Client, chainState.ChainDB,
		summary.GetBlockRoot(),
		c.config.SnapshotFactory,
		codeQueue, c.config.RequestSize,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM state syncer: %w", err)
	}

	syncers := []types.Syncer{blockSyncer, codeSyncer, stateSyncer}

	if c.config.Extender != nil {
		extenderSyncer, err := c.config.Extender.CreateSyncer(c.config.Client, chainState.VerDB, summary)
		if err != nil {
			return nil, fmt.Errorf("failed to create extender syncer: %w", err)
		}
		syncers = append(syncers, extenderSyncer)
	}

	for _, s := range syncers {
		if err := registry.Register(s); err != nil {
			return nil, fmt.Errorf("failed to register %s syncer: %w", s.Name(), err)
		}
	}

	return registry, nil
}
