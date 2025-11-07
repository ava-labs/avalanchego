// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
)

var (
	errBlockNotFound       = errors.New("block not found in state")
	errInvalidBlockType    = errors.New("invalid block wrapper type")
	errBlockHashMismatch   = errors.New("block hash mismatch")
	errBlockHeightMismatch = errors.New("block height mismatch")
	errFinalizeCancelled   = errors.New("finalize cancelled")
	errCommitMarkers       = errors.New("failed to commit VM markers")
)

// finalizer handles VM state finalization after sync completes.
type finalizer struct {
	chain              *eth.Ethereum
	state              *chain.State
	acceptor           BlockAcceptor
	verDB              *versiondb.Database
	metadataDB         database.Database
	extender           syncpkg.Extender
	lastAcceptedHeight uint64
}

// newFinalizer creates a new finalizer with the given dependencies.
func newFinalizer(
	chain *eth.Ethereum,
	state *chain.State,
	acceptor BlockAcceptor,
	verDB *versiondb.Database,
	metadataDB database.Database,
	extender syncpkg.Extender,
	lastAcceptedHeight uint64,
) *finalizer {
	return &finalizer{
		chain:              chain,
		state:              state,
		acceptor:           acceptor,
		verDB:              verDB,
		metadataDB:         metadataDB,
		extender:           extender,
		lastAcceptedHeight: lastAcceptedHeight,
	}
}

// finalize updates disk and memory pointers so the VM is prepared for bootstrapping.
// Executes any shared memory operations from the atomic trie to shared memory.
func (f *finalizer) finalize(ctx context.Context, summary message.Syncable) error {
	stateBlock, err := f.state.GetBlock(ctx, ids.ID(summary.GetBlockHash()))
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
	f.chain.BloomIndexer().AddCheckpoint(parentHeight/params.BloomBitsBlocks, parentHash)

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errFinalizeCancelled, err)
	}
	if err := f.chain.BlockChain().ResetToStateSyncedBlock(block); err != nil {
		return err
	}

	if f.extender != nil {
		if err := f.extender.OnFinishBeforeCommit(f.lastAcceptedHeight, summary); err != nil {
			return err
		}
	}

	if err := f.commitMarkers(summary); err != nil {
		return fmt.Errorf("%w: height=%d, hash=%s: %w", errCommitMarkers, block.NumberU64(), block.Hash(), err)
	}

	if err := f.state.SetLastAcceptedBlock(wrappedBlock); err != nil {
		return err
	}

	if f.extender != nil {
		if err := f.extender.OnFinishAfterCommit(block.NumberU64()); err != nil {
			return err
		}
	}

	return nil
}

// commitMarkers updates VM database markers atomically.
func (f *finalizer) commitMarkers(summary message.Syncable) error {
	id := ids.ID(summary.GetBlockHash())
	if err := f.acceptor.PutLastAcceptedID(id); err != nil {
		return err
	}
	if err := f.metadataDB.Delete(stateSyncSummaryKey); err != nil {
		return err
	}
	return f.verDB.Commit()
}
