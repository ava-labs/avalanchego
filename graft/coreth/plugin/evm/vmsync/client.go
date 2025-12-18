// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/blocksync"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/statesync"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

// BlocksToFetch is the number of the block parents the state syncs to.
// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
const BlocksToFetch = 256

var (
	stateSyncSummaryKey = []byte("stateSyncSummary")

	errSkipSync            = errors.New("skip sync")
	errBlockNotFound       = errors.New("block not found in state")
	errInvalidBlockType    = errors.New("invalid block wrapper type")
	errBlockHashMismatch   = errors.New("block hash mismatch")
	errBlockHeightMismatch = errors.New("block height mismatch")
	errCommitCancelled     = errors.New("commit cancelled")
	errCommitMarkers       = errors.New("failed to commit VM markers")
)

// BlockAcceptor provides a mechanism to update the last accepted block ID during state synchronization.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// EthBlockWrapper can be implemented by a concrete block wrapper type to
// return *types.Block, which is needed to update chain pointers at the
// end of the sync operation. It also provides Accept/Reject/Verify operations
// for deferred processing during dynamic state sync.
type EthBlockWrapper interface {
	GetEthBlock() *types.Block
	Accept(context.Context) error
	Reject(context.Context) error
	Verify(context.Context) error
}

// Committer commits sync results to the VM, preparing it for bootstrapping.
type Committer interface {
	Commit(ctx context.Context, summary message.Syncable) error
}

// Executor defines how state sync is executed.
// Implementations handle the sync lifecycle differently based on sync mode.
type Executor interface {
	// Execute runs the sync process and blocks until completion or error.
	Execute(ctx context.Context, summary message.Syncable) error

	// OnBlockAccepted handles a block accepted during sync.
	OnBlockAccepted(EthBlockWrapper) (bool, error)

	// OnBlockRejected handles a block rejected during sync.
	OnBlockRejected(EthBlockWrapper) (bool, error)

	// OnBlockVerified handles a block verified during sync.
	OnBlockVerified(EthBlockWrapper) (bool, error)
}

var _ Committer = (*client)(nil)

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
	// DynamicStateSyncEnabled toggles dynamic vs static state sync orchestration.
	DynamicStateSyncEnabled bool

	// PivotInterval advances the sync target every N blocks.
	PivotInterval uint64
}

type client struct {
	config           *ClientConfig
	resumableSummary message.Syncable
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	err              error
	stateSyncOnce    sync.Once  // ensures only one state sync can be in progress at a time
	executor         Executor   // executor manages sync execution (static or dynamic)
}

func NewClient(config *ClientConfig) Client {
	return &client{config: config}
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
	// OnEngineAccept should be called by the engine when a block is accepted.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineAccept(EthBlockWrapper) (bool, error)
	// OnEngineReject should be called by the engine when a block is rejected.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineReject(EthBlockWrapper) (bool, error)
	// OnEngineVerify should be called by the engine when a block is verified.
	// Returns true if the block was enqueued for deferred processing, false otherwise.
	OnEngineVerify(EthBlockWrapper) (bool, error)
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

// OnEngineAccept delegates to the executor if active.
func (c *client) OnEngineAccept(b EthBlockWrapper) (bool, error) {
	if c.executor == nil {
		return false, nil
	}
	return c.executor.OnBlockAccepted(b)
}

// OnEngineReject delegates to the executor if active.
func (c *client) OnEngineReject(b EthBlockWrapper) (bool, error) {
	if c.executor == nil {
		return false, nil
	}
	return c.executor.OnBlockRejected(b)
}

// OnEngineVerify delegates to the executor if active.
func (c *client) OnEngineVerify(b EthBlockWrapper) (bool, error) {
	if c.executor == nil {
		return false, nil
	}
	return c.executor.OnBlockVerified(b)
}

func (c *client) Shutdown() error {
	c.signalDone(context.Canceled)
	c.wg.Wait()
	return nil
}

// Error returns a non-nil error if one occurred during the sync.
func (c *client) Error() error {
	return c.err
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (c *client) acceptSyncSummary(proposedSummary message.Syncable) (block.StateSyncMode, error) {
	// If dynamic sync is already running, treat new summaries as target updates.
	if ds, ok := c.executor.(*dynamicExecutor); ok && ds.CurrentState() == StateRunning {
		if err := ds.UpdateSyncTarget(proposedSummary); err != nil {
			return block.StateSyncSkipped, err
		}
		return block.StateSyncDynamic, nil
	}

	isResume := c.resumableSummary != nil &&
		proposedSummary.GetBlockHash() == c.resumableSummary.GetBlockHash()
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		if c.config.LastAcceptedHeight+c.config.MinBlocks > proposedSummary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", c.config.LastAcceptedHeight,
				"syncableHeight", proposedSummary.Height(),
			)
			return block.StateSyncSkipped, nil
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

	// Update the current state sync summary key in the database.
	if err := c.config.MetadataDB.Put(stateSyncSummaryKey, proposedSummary.Bytes()); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := c.config.VerDB.Commit(); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to commit db: %w", err)
	}

	log.Info("Starting state sync", "summary", proposedSummary.GetBlockHash().Hex(), "height", proposedSummary.Height())
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	registry, err := c.newSyncerRegistry(proposedSummary)
	if err != nil {
		return block.StateSyncSkipped, err
	}

	var (
		executor Executor
		mode     block.StateSyncMode
	)
	if c.config.DynamicStateSyncEnabled {
		executor = newDynamicExecutor(registry, c, c.config.PivotInterval)
		mode = block.StateSyncDynamic
	} else {
		executor = newStaticExecutor(registry, c)
		mode = block.StateSyncStatic
	}

	c.executor = executor
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		err := executor.Execute(ctx, proposedSummary)
		c.signalDone(err)
	}()

	log.Info("state sync started", "mode", mode.String(), "summary", proposedSummary.GetBlockHash().Hex(), "height", proposedSummary.Height())
	return mode, nil
}

// signalDone sets the terminal error exactly once, signals completion to the engine.
func (c *client) signalDone(err error) {
	c.stateSyncOnce.Do(func() {
		c.err = err
		if c.cancel != nil {
			c.cancel()
		}
		close(c.config.StateSyncDone)
	})
}

// Commit implements Committer. It resets the blockchain to the synced block,
// preparing it for execution, and updates disk and memory pointers so the VM
// is ready for bootstrapping. Also executes any shared memory operations from
// the atomic trie to shared memory.
func (c *client) Commit(ctx context.Context, summary message.Syncable) error {
	stateBlock, err := c.config.State.GetBlock(ctx, ids.ID(summary.GetBlockHash()))
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

	// BloomIndexer needs to know that some parts of the chain are not available
	// and cannot be indexed. This is done by calling [AddCheckpoint] here.
	// Since the indexer uses sections of size [params.BloomBitsBlocks] (= 4096),
	// each block is indexed in section number [blockNumber/params.BloomBitsBlocks].
	// To allow the indexer to start with the block we just synced to,
	// we create a checkpoint for its parent.
	// Note: This requires assuming the synced block height is divisible
	// by [params.BloomBitsBlocks].
	parentHeight := block.NumberU64() - 1
	parentHash := block.ParentHash()
	c.config.Chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errCommitCancelled, err)
	}
	if err := c.config.Chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
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

	if err := c.config.State.SetLastAcceptedBlock(wrappedBlock); err != nil {
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
	id := ids.ID(summary.GetBlockHash())
	if err := c.config.Acceptor.PutLastAcceptedID(id); err != nil {
		return err
	}
	if err := c.config.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return c.config.VerDB.Commit()
}

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
