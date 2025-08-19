// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	synccommon "github.com/ava-labs/coreth/sync"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
)

const blocksPerRequest = 32

var _ synccommon.Syncer = (*blockSyncer)(nil)

type Config struct {
	FromHash      common.Hash // `FromHash` is the most recent
	FromHeight    uint64
	BlocksToFetch uint64 // Includes the `FromHash` block
}

type blockSyncer struct {
	db     ethdb.Database
	client statesyncclient.Client
	config Config
}

func NewSyncer(client statesyncclient.Client, db ethdb.Database, config Config) (*blockSyncer, error) {
	return &blockSyncer{
		client: client,
		db:     db,
		config: config,
	}, nil
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
func (s *blockSyncer) Sync(ctx context.Context) error {
	nextHash := s.config.FromHash
	nextHeight := s.config.FromHeight
	blocksToFetch := s.config.BlocksToFetch

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
