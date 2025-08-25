// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/blocksync"
	"github.com/ava-labs/coreth/sync/statesync"

	synccommon "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

const (
	// BlocksToFetch is the number of the block parents the state syncs to.
	// The last 256 block hashes are necessary to support the BLOCKHASH opcode.
	BlocksToFetch = 256

	atomicStateSyncOperationName = "Atomic State Sync"
	blockSyncOperationName       = "Block Sync"
	evmStateSyncOperationName    = "EVM State Sync"
)

var stateSyncSummaryKey = []byte("stateSyncSummary")

// BlockAcceptor provides a mechanism to update the last accepted block ID during state synchronization.
// This interface is used by the state sync process to ensure the blockchain state
// is properly updated when new blocks are synchronized from the network.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// EthBlockWrapper can be implemented by a concrete block wrapper type to
// return *types.Block, which is needed to update chain pointers at the
// end of the sync operation.
type EthBlockWrapper interface {
	GetEthBlock() *types.Block
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
	Extender      synccommon.Extender
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
	*ClientConfig

	resumableSummary message.Syncable

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State Sync results
	summary message.Syncable
	err     error
}

func NewClient(config *ClientConfig) Client {
	return &client{
		ClientConfig: config,
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
func (client *client) StateSyncEnabled(context.Context) (bool, error) {
	return client.Enabled, nil
}

// GetOngoingSyncStateSummary returns a state summary that was previously started
// and not finished, and sets [resumableSummary] if one was found.
// Returns [database.ErrNotFound] if no ongoing summary is found or if [client.skipResume] is true.
func (client *client) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	if client.SkipResume {
		return nil, database.ErrNotFound
	}

	summaryBytes, err := client.MetadataDB.Get(stateSyncSummaryKey)
	if err != nil {
		return nil, err // includes the [database.ErrNotFound] case
	}

	summary, err := client.Parser.Parse(summaryBytes, client.acceptSyncSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved state sync summary to SyncSummary: %w", err)
	}
	client.resumableSummary = summary
	return summary, nil
}

// ClearOngoingSummary clears any marker of an ongoing state sync summary
func (client *client) ClearOngoingSummary() error {
	if err := client.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return fmt.Errorf("failed to clear ongoing summary: %w", err)
	}
	if err := client.VerDB.Commit(); err != nil {
		return fmt.Errorf("failed to commit db while clearing ongoing summary: %w", err)
	}

	return nil
}

// ParseStateSummary parses [summaryBytes] to [commonEng.Summary]
func (client *client) ParseStateSummary(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return client.Parser.Parse(summaryBytes, client.acceptSyncSummary)
}

func (client *client) stateSync(ctx context.Context) error {
	// Create and register all syncers.
	registry := NewSyncerRegistry()

	if err := client.registerSyncers(registry); err != nil {
		return err
	}

	// Run all registered syncers sequentially.
	return registry.RunSyncerTasks(ctx, client)
}

func (client *client) registerSyncers(registry *SyncerRegistry) error {
	// Register block syncer.
	syncer, err := client.createBlockSyncer(client.summary.GetBlockHash(), client.summary.Height())
	if err != nil {
		return fmt.Errorf("failed to create block syncer: %w", err)
	}
	if err := registry.Register(blockSyncOperationName, syncer); err != nil {
		return fmt.Errorf("failed to register block syncer: %w", err)
	}

	// Register EVM state syncer.
	evmSyncer, err := client.createEVMSyncer()
	if err != nil {
		return fmt.Errorf("failed to create EVM syncer: %w", err)
	}

	if err := registry.Register(evmStateSyncOperationName, evmSyncer); err != nil {
		return fmt.Errorf("failed to register EVM syncer: %w", err)
	}

	// Register atomic syncer.
	if client.Extender != nil {
		atomicSyncer, err := client.createAtomicSyncer()
		if err != nil {
			return fmt.Errorf("failed to create atomic syncer: %w", err)
		}

		if err := registry.Register(atomicStateSyncOperationName, atomicSyncer); err != nil {
			return fmt.Errorf("failed to register atomic syncer: %w", err)
		}
	}

	return nil
}

func (client *client) createBlockSyncer(fromHash common.Hash, fromHeight uint64) (synccommon.Syncer, error) {
	return blocksync.NewSyncer(client.Client, client.ChainDB, blocksync.Config{
		FromHash:      fromHash,
		FromHeight:    fromHeight,
		BlocksToFetch: BlocksToFetch,
	})
}

func (client *client) createEVMSyncer() (synccommon.Syncer, error) {
	return statesync.NewSyncer(client.Client, client.ChainDB, client.summary.GetBlockRoot(), statesync.NewDefaultConfig(client.RequestSize))
}

func (client *client) createAtomicSyncer() (synccommon.Syncer, error) {
	return client.Extender.CreateSyncer(client.Client, client.VerDB, client.summary)
}

// acceptSyncSummary returns true if sync will be performed and launches the state sync process
// in a goroutine.
func (client *client) acceptSyncSummary(proposedSummary message.Syncable) (block.StateSyncMode, error) {
	isResume := client.resumableSummary != nil &&
		proposedSummary.GetBlockHash() == client.resumableSummary.GetBlockHash()
	if !isResume {
		// Skip syncing if the blockchain is not significantly ahead of local state,
		// since bootstrapping would be faster.
		// (Also ensures we don't sync to a height prior to local state.)
		if client.LastAcceptedHeight+client.MinBlocks > proposedSummary.Height() {
			log.Info(
				"last accepted too close to most recent syncable block, skipping state sync",
				"lastAccepted", client.LastAcceptedHeight,
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
		<-snapshot.WipeSnapshot(client.ChainDB, true)
		// Reset the snapshot generator here so that when state sync completes, snapshots will not attempt to read an
		// invalid generator.
		// Note: this must be called after WipeSnapshot is called so that we do not invalidate a partially generated snapshot.
		snapshot.ResetSnapshotGeneration(client.ChainDB)
	}
	client.summary = proposedSummary

	// Update the current state sync summary key in the database
	// Note: this must be performed after WipeSnapshot finishes so that we do not start a state sync
	// session from a partially wiped snapshot.
	if err := client.MetadataDB.Put(stateSyncSummaryKey, proposedSummary.Bytes()); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to write state sync summary key to disk: %w", err)
	}
	if err := client.VerDB.Commit(); err != nil {
		return block.StateSyncSkipped, fmt.Errorf("failed to commit db: %w", err)
	}

	log.Info("Starting state sync", "summary", proposedSummary)

	// create a cancellable ctx for the state sync goroutine
	ctx, cancel := context.WithCancel(context.Background())
	client.cancel = cancel
	client.wg.Add(1) // track the state sync goroutine so we can wait for it on shutdown
	go func() {
		defer client.wg.Done()
		defer cancel()

		if err := client.stateSync(ctx); err != nil {
			client.err = err
		} else {
			client.err = client.finishSync()
		}
		// notify engine regardless of whether err == nil,
		// this error will be propagated to the engine when it calls
		// vm.SetState(snow.Bootstrapping)
		log.Info("stateSync completed, notifying engine", "err", client.err)
		close(client.StateSyncDone)
	}()
	return block.StateSyncStatic, nil
}

func (client *client) Shutdown() error {
	if client.cancel != nil {
		client.cancel()
	}
	client.wg.Wait() // wait for the background goroutine to exit
	return nil
}

// finishSync is responsible for updating disk and memory pointers so the VM is prepared
// for bootstrapping. Executes any shared memory operations from the atomic trie to shared memory.
func (client *client) finishSync() error {
	stateBlock, err := client.State.GetBlock(context.TODO(), ids.ID(client.summary.GetBlockHash()))
	if err != nil {
		return fmt.Errorf("could not get block by hash from client state: %s", client.summary.GetBlockHash())
	}

	wrapper, ok := stateBlock.(*chain.BlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to *chain.BlockWrapper", wrapper)
	}
	wrappedBlock := wrapper.Block

	evmBlockGetter, ok := wrappedBlock.(EthBlockWrapper)
	if !ok {
		return fmt.Errorf("could not convert block(%T) to evm.EthBlockWrapper", stateBlock)
	}

	block := evmBlockGetter.GetEthBlock()

	if block.Hash() != client.summary.GetBlockHash() {
		return fmt.Errorf("attempted to set last summary block to unexpected block hash: (%s != %s)", block.Hash(), client.summary.GetBlockHash())
	}
	if block.NumberU64() != client.summary.Height() {
		return fmt.Errorf("attempted to set last summary block to unexpected block number: (%d != %d)", block.NumberU64(), client.summary.Height())
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
	client.Chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err := client.Chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
		return err
	}

	if client.Extender != nil {
		if err := client.Extender.OnFinishBeforeCommit(client.LastAcceptedHeight, client.summary); err != nil {
			return err
		}
	}

	if err := client.commitVMMarkers(); err != nil {
		return fmt.Errorf("error updating vm markers, height=%d, hash=%s, err=%w", block.NumberU64(), block.Hash(), err)
	}

	if err := client.State.SetLastAcceptedBlock(wrappedBlock); err != nil {
		return err
	}

	if client.Extender != nil {
		return client.Extender.OnFinishAfterCommit(block.NumberU64())
	}

	return nil
}

// commitVMMarkers updates the following markers in the VM's database
// and commits them atomically:
// - updates atomic trie so it will have necessary metadata for the last committed root
// - updates atomic trie so it will resume applying operations to shared memory on initialize
// - updates lastAcceptedKey
// - removes state sync progress markers
func (client *client) commitVMMarkers() error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block to [vm.syncSummary] when ApplyToSharedMemory
	// is called.
	id := ids.ID(client.summary.GetBlockHash())
	if err := client.Acceptor.PutLastAcceptedID(id); err != nil {
		return err
	}
	if err := client.MetadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return client.VerDB.Commit()
}

// Error returns a non-nil error if one occurred during the sync.
func (client *client) Error() error { return client.err }
