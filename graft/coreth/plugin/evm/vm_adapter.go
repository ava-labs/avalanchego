// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/libevm/common"
	libevmsnapshot "github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/vm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/chain"
)


var (
	_ vm.VMAdapter              = (*corethAdapter)(nil)
	_ handlers.SnapshotProvider = (*snapshotProviderAdapter)(nil)
	_ handlers.SyncDataProvider = (*syncDataProviderAdapter)(nil)
)

// BlockAcceptor provides a mechanism to update the last accepted block ID.
type BlockAcceptor interface {
	PutLastAcceptedID(ids.ID) error
}

// Ensure VM implements BlockAcceptor for VMAdapter.
var _ BlockAcceptor = (*VM)(nil)

// ============================================================================
// Coreth VM Adapter Implementation
// ============================================================================

// corethAdapter implements vm.VMAdapter for Coreth.
type corethAdapter struct {
	ethereum   *eth.Ethereum
	state      *chain.State
	chainDB    ethdb.Database
	acceptor   BlockAcceptor
	verDB      *versiondb.Database
	metadataDB database.Database
}

func NewCorethAdapter(ethereum *eth.Ethereum, state *chain.State, chainDB ethdb.Database, acceptor BlockAcceptor, verDB *versiondb.Database, metadataDB database.Database) vm.VMAdapter {
	return &corethAdapter{
		ethereum:   ethereum,
		state:      state,
		chainDB:    chainDB,
		acceptor:   acceptor,
		verDB:      verDB,
		metadataDB: metadataDB,
	}
}

// GetChainState returns the current VM chain state.
func (c *corethAdapter) GetChainState() (vm.ChainState, error) {
	return vm.ChainState{
		LastAcceptedHeight: c.state.LastAcceptedBlock().Height(),
		ChainDB:            c.chainDB,
		VerDB:              c.verDB,
		MetadataDB:         c.metadataDB,
		State:              c.state,
	}, nil
}

// GetBlock retrieves a block by its ID from the VM state.
func (c *corethAdapter) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	return c.state.GetBlock(ctx, blkID)
}

// ProcessSyncedBlock processes a block that was synced.
// This handles blockchain reset and bloom indexer updates.
func (c *corethAdapter) ProcessSyncedBlock(block vm.BlockWrapper) error {
	ethBlock := block.GetEthBlock()

	// Add checkpoint for bloom indexer.
	parentHeight := ethBlock.NumberU64() - 1
	parentHash := ethBlock.ParentHash()
	c.ethereum.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	// Reset blockchain to synced block.
	return c.ethereum.BlockChain().ResetToStateSyncedBlock(ethBlock)
}

// SetLastAcceptedBlock sets the last accepted block on the VM state.
func (c *corethAdapter) SetLastAcceptedBlock(block snowman.Block) error {
	return c.state.SetLastAcceptedBlock(block)
}

// UpdateChainPointers updates chain pointers after sync.
func (c *corethAdapter) UpdateChainPointers(summary vm.StateSummary) error {
	id := ids.ID(summary.GetBlockHash())
	return c.acceptor.PutLastAcceptedID(id)
}

// WipeSnapshot wipes the snapshot as part of sync preparation.
func (c *corethAdapter) WipeSnapshot() error {
	snapshot.WipeSnapshot(c.chainDB, true)
	return nil
}

// ResetSnapshotGenerator resets the snapshot generator.
func (c *corethAdapter) ResetSnapshotGenerator() error {
	snapshot.ResetSnapshotGeneration(c.chainDB)
	return nil
}

// ============================================================================
// Snapshot Provider Adapter (for handlers package)
// ============================================================================

// snapshotProviderAdapter wraps coreth BlockChain to implement [handlers.SnapshotProvider].
type snapshotProviderAdapter struct {
	blockchain *core.BlockChain
}

// NewSnapshotProviderAdapter creates a new adapter that wraps a [core.BlockChain]
// to implement [handlers.SnapshotProvider] with [handlers.SnapshotTree].
func NewSnapshotProviderAdapter(blockchain *core.BlockChain) handlers.SnapshotProvider {
	return &snapshotProviderAdapter{blockchain: blockchain}
}

func (s *snapshotProviderAdapter) Snapshots() handlers.SnapshotTree {
	return &snapshotTreeAdapter{tree: s.blockchain.Snapshots()}
}

// snapshotTreeAdapter wraps coreth [*snapshot.Tree] to implement [handlers.SnapshotTree].
type snapshotTreeAdapter struct {
	tree *snapshot.Tree
}

func (s *snapshotTreeAdapter) DiskAccountIterator(seek common.Hash) libevmsnapshot.AccountIterator {
	return s.tree.DiskAccountIterator(seek)
}

func (s *snapshotTreeAdapter) DiskStorageIterator(account common.Hash, seek common.Hash) libevmsnapshot.StorageIterator {
	return s.tree.DiskStorageIterator(account, seek)
}

// syncDataProviderAdapter wraps coreth BlockChain to implement [handlers.SyncDataProvider].
type syncDataProviderAdapter struct {
	blockchain *core.BlockChain
}

// NewSyncDataProviderAdapter creates a new adapter that wraps a [core.BlockChain]
// to implement [handlers.SyncDataProvider].
func NewSyncDataProviderAdapter(blockchain *core.BlockChain) handlers.SyncDataProvider {
	return &syncDataProviderAdapter{blockchain: blockchain}
}

func (s *syncDataProviderAdapter) GetBlock(hash common.Hash, number uint64) *types.Block {
	return s.blockchain.GetBlock(hash, number)
}

func (s *syncDataProviderAdapter) Snapshots() handlers.SnapshotTree {
	return &snapshotTreeAdapter{tree: s.blockchain.Snapshots()}
}

