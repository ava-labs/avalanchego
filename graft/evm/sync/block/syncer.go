// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

const (
	blocksPerRequest = 32

	// SyncerID is the stable identifier for the block syncer.
	SyncerID = "state_block_sync"
)

var (
	_                        types.Syncer = (*Syncer)(nil)
	errBlocksToFetchRequired              = errors.New("blocksToFetch must be > 0")
	errFromHashRequired                   = errors.New("fromHash must be non-zero when fromHeight > 0")
)

type syncTarget struct {
	hash   common.Hash
	height uint64
}

type Syncer struct {
	db            ethdb.Database
	client        client.Client
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64

	targetMu     sync.Mutex
	latestTarget *syncTarget
}

func NewSyncer(client client.Client, db ethdb.Database, fromHash common.Hash, fromHeight uint64, blocksToFetch uint64) (*Syncer, error) {
	if blocksToFetch == 0 {
		return nil, errBlocksToFetchRequired
	}

	if (fromHash == common.Hash{}) && fromHeight > 0 {
		return nil, errFromHashRequired
	}

	return &Syncer{
		client:        client,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: blocksToFetch,
	}, nil
}

// Name returns the human-readable name for this sync task.
func (*Syncer) Name() string {
	return "Block Syncer"
}

// ID returns the stable identifier for this sync task.
func (*Syncer) ID() string {
	return SyncerID
}

// Sync fetches blocks for the initial target and, if a materially newer
// target arrived via UpdateTarget during the first pass, performs one
// bounded catch-up pass. At most two passes are executed per Sync call.
func (s *Syncer) Sync(ctx context.Context) error {
	if err := s.syncWindow(ctx, s.fromHash, s.fromHeight); err != nil {
		return err
	}

	catchUp := s.consumeTarget(s.fromHeight)
	if catchUp == nil {
		return nil
	}
	return s.syncWindow(ctx, catchUp.hash, catchUp.height)
}

// UpdateTarget records a newer sync target. It is thread-safe, non-blocking,
// and monotonic - targets at or below the current ceiling are ignored.
func (s *Syncer) UpdateTarget(newTarget message.Syncable) error {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()

	newHeight := newTarget.Height()
	ceiling := s.fromHeight
	if s.latestTarget != nil && s.latestTarget.height > ceiling {
		ceiling = s.latestTarget.height
	}
	if newHeight <= ceiling {
		return nil
	}
	s.latestTarget = &syncTarget{
		hash:   newTarget.GetBlockHash(),
		height: newHeight,
	}
	return nil
}

// consumeTarget atomically reads and clears latestTarget if the drift from
// passStartHeight is material (greater than blocksToFetch). Returns nil
// when no catch-up is needed.
func (s *Syncer) consumeTarget(passStartHeight uint64) *syncTarget {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()

	t := s.latestTarget
	// A catch-up pass is only worthwhile when the new target has drifted
	// beyond what the previous pass already covered (blocksToFetch).
	driftExceedsWindow := t != nil && t.height > passStartHeight && t.height-passStartHeight > s.blocksToFetch
	if !driftExceedsWindow {
		return nil
	}
	s.latestTarget = nil
	return t
}

// syncWindow fetches up to blocksToFetch blocks ending at targetHash/targetHeight.
// Blocks already on disk are skipped - remaining blocks are fetched from peers.
//
// TODO(powerslider): We could inspect the database more accurately to ensure we never fetch
// any blocks that are locally available. We could also prevent overrequesting blocks, if
// the number of blocks needed to be fetched isn't a multiple of blocksPerRequest.
func (s *Syncer) syncWindow(ctx context.Context, targetHash common.Hash, targetHeight uint64) error {
	nextHash := targetHash
	nextHeight := targetHeight
	blocksToFetch := s.blocksToFetch

	// First, check for blocks already available on disk so we don't
	// request them from peers.
	for blocksToFetch > 0 {
		blk := rawdb.ReadBlock(s.db, nextHash, nextHeight)
		if blk == nil {
			break
		}
		nextHash = blk.ParentHash()
		nextHeight--
		blocksToFetch--
	}

	// Fetch any blocks we couldn't find on disk from peers and write
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
