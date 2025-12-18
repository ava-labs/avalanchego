// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	statesyncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

const blocksPerRequest = 32

var (
	_                        syncpkg.Syncer = (*BlockSyncer)(nil)
	errBlocksToFetchRequired                = errors.New("blocksToFetch must be > 0")
	errFromHashRequired                     = errors.New("fromHash must be non-zero when fromHeight > 0")
)

type BlockSyncer struct {
	db            ethdb.Database
	client        statesyncclient.Client
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64
}

func NewSyncer(client statesyncclient.Client, db ethdb.Database, fromHash common.Hash, fromHeight uint64, blocksToFetch uint64) (*BlockSyncer, error) {
	if blocksToFetch == 0 {
		return nil, errBlocksToFetchRequired
	}

	if (fromHash == common.Hash{}) && fromHeight > 0 {
		return nil, errFromHashRequired
	}

	return &BlockSyncer{
		client:        client,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: blocksToFetch,
	}, nil
}

// Name returns the human-readable name for this sync task.
func (*BlockSyncer) Name() string {
	return "Block Syncer"
}

// ID returns the stable identifier for this sync task.
func (*BlockSyncer) ID() string {
	return "state_block_sync"
}

// Sync fetches (up to) BlocksToFetch blocks from peers
// using Client and writes them to disk.
// the process begins with FromHash and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
//
// TODO: We could inspect the database more accurately to ensure we never fetch
// any blocks that are locally available.
// We could also prevent overrequesting blocks, if the number of blocks needed
// to be fetched isn't a multiple of blocksPerRequest.
func (s *BlockSyncer) Sync(ctx context.Context) error {
	nextHash := s.fromHash
	nextHeight := s.fromHeight
	blocksToFetch := s.blocksToFetch

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for blocksToFetch > 0 {
		blk := rawdb.ReadBlock(s.db, nextHash, nextHeight)
		if blk == nil {
			// block was not found
			break
		}

		// block exists
		nextHash = blk.ParentHash()
		nextHeight--
		blocksToFetch--
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := s.db.NewBatch()
	for fetched := uint64(0); fetched < blocksToFetch && (nextHash != common.Hash{}); {
		log.Info("fetching blocks from peer", "fetched", fetched, "total", blocksToFetch)
		blocks, err := s.client.GetBlocks(ctx, nextHash, nextHeight, blocksPerRequest)
		if err != nil {
			return fmt.Errorf("could not get blocks from peer: err: %w, nextHash: %s, fetched: %d", err, nextHash, fetched)
		}
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			fetched++
			nextHash = block.ParentHash()
			nextHeight--
		}
	}

	log.Info("fetched blocks from peer", "total", blocksToFetch)
	return batch.Write()
}

func (*BlockSyncer) UpdateTarget(_ message.Syncable) error {
	return nil
}
