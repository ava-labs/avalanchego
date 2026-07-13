// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/vms/evm/sync/types"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	evmtypes "github.com/ava-labs/libevm/core/types"
)

var (
	_ types.Syncer = (*Syncer)(nil)

	errBlocksToFetchRequired = errors.New("blocksToFetch must be greater than zero")
	errFromHashRequired      = errors.New("fromHash must be non-zero when fromHeight is greater than zero")
	errEmptyResponse         = errors.New("empty block response")
	errTooManyBlocks         = errors.New("more blocks returned than requested")
	errDecodeBlock           = errors.New("failed to decode block")
	errBlockHashMismatch     = errors.New("block does not hash to the expected value")
)

// Syncer fetches a contiguous run of blocks by walking parents from a known tip
// and writes them to db. It skips any suffix already on disk, verifies every
// response links tip-to-parent, and re-requests on failure.
type Syncer struct {
	client        *Client
	db            ethdb.Database
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64
}

// NewSyncer returns a [Syncer] that fetches blocksToFetch blocks ending at
// (fromHash, fromHeight) and writes them into db, fetching from peers through c.
func NewSyncer(c *Client, db ethdb.Database, fromHash common.Hash, fromHeight, blocksToFetch uint64) (*Syncer, error) {
	if blocksToFetch == 0 {
		return nil, errBlocksToFetchRequired
	}
	if (fromHash == common.Hash{}) && fromHeight > 0 {
		return nil, errFromHashRequired
	}
	return &Syncer{
		client:        c,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: blocksToFetch,
	}, nil
}

// Name returns a human-readable name for logging.
func (*Syncer) Name() string { return "Block Syncer" }

// ID returns the stable identifier used for deduplication and metrics.
func (*Syncer) ID() string { return "state_block_sync" }

// Sync walks parents from fromHash, skips blocks already on disk, and fetches
// the rest from peers in batches. It runs until blocksToFetch are satisfied,
// the genesis parent is reached, or ctx ends.
func (s *Syncer) Sync(ctx context.Context) error {
	nextHash := s.fromHash
	nextHeight := s.fromHeight
	toFetch := s.blocksToFetch

	// Skip any suffix already on disk so we do not re-request it.
	for toFetch > 0 {
		blk := rawdb.ReadBlock(s.db, nextHash, nextHeight)
		if blk == nil {
			break
		}
		nextHash = blk.ParentHash()
		nextHeight--
		toFetch--
	}

	batch := s.db.NewBatch()
	for toFetch > 0 && nextHash != (common.Hash{}) {
		if err := ctx.Err(); err != nil {
			return err
		}

		parents := uint16(min(toFetch, uint64(MaxParentsPerRequest)))
		blocks, err := getBlocks(ctx, s.client, nextHash, nextHeight, parents)
		if err != nil {
			return fmt.Errorf("could not get blocks at %s: %w", nextHash, err)
		}

		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
			nextHash = block.ParentHash()
			nextHeight--
			toFetch--
		}
	}

	return batch.Write()
}

// getBlocks requests up to numParents blocks ending at (hash, height), verifies
// the returned chain links back from hash, scores the peer, and re-requests on
// any network or verification failure until ctx ends.
func getBlocks(ctx context.Context, c *Client, hash common.Hash, height uint64, numParents uint16) ([]*evmtypes.Block, error) {
	req := &syncpb.GetBlockRequest{Height: height, NumParents: uint32(numParents)}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp := &syncpb.GetBlockResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		blocks, err := verifyBlocks(hash, numParents, resp.GetBlocks())
		if err != nil {
			outcome.Failure()
			log.Debug("invalid block response, re-requesting", "err", err)
			continue
		}

		outcome.Success()
		return blocks, nil
	}
}

// verifyBlocks decodes raw and reports whether it is the parent chain ending at
// hash, in tip-first order.
func verifyBlocks(hash common.Hash, numParents uint16, raw [][]byte) ([]*evmtypes.Block, error) {
	if len(raw) == 0 {
		return nil, errEmptyResponse
	}
	if len(raw) > int(numParents) {
		return nil, fmt.Errorf("%w: got %d requested %d", errTooManyBlocks, len(raw), numParents)
	}

	blocks := make([]*evmtypes.Block, len(raw))
	want := hash
	for i, blockBytes := range raw {
		block := new(evmtypes.Block)
		if err := rlp.DecodeBytes(blockBytes, block); err != nil {
			return nil, fmt.Errorf("%w at index %d: %w", errDecodeBlock, i, err)
		}
		if got := block.Hash(); got != want {
			return nil, fmt.Errorf("%w at index %d: got %s expected %s", errBlockHashMismatch, i, got, want)
		}
		blocks[i] = block
		want = block.ParentHash()
	}
	return blocks, nil
}
