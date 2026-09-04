// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// A Parser decodes a block and verifies that the body matches the header.
type Parser func([]byte) (*types.Block, error)

// A Syncer downloads a contiguous chain of blocks from peers and persists it,
// writing each fetched block with [rawdb.WriteBlock] and marking it canonical
// with [rawdb.WriteCanonicalHash].
type Syncer struct {
	log           logging.Logger
	client        *Client
	db            ethdb.Database
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64
	parseBlock    Parser
}

// NewSyncer returns a [Syncer] that fetches the blocks with heights in the
// half-open interval (fromHeight-blocksToFetch, fromHeight]. fromHash
// identifies the block at fromHeight and, through its ancestry, every other
// fetched block. blocksToFetch is capped at fromHeight+1 since no blocks
// exist below genesis.
func NewSyncer(
	log logging.Logger,
	c *Client,
	db ethdb.Database,
	parse Parser,
	fromHash common.Hash,
	fromHeight uint64,
	blocksToFetch uint64,
) *Syncer {
	return &Syncer{
		log:           log,
		client:        c,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: min(blocksToFetch, fromHeight+1),
		parseBlock:    parse,
	}
}

// maxBlocksPerResponse is the most blocks one response can carry, the
// requested block plus [maxParentsPerRequest] parents.
const maxBlocksPerResponse = maxParentsPerRequest + 1

// Sync returns once all blocksToFetch blocks are persisted or ctx ends.
//
// Sync may be called again after an interruption.
func (s *Syncer) Sync(ctx context.Context) error {
	nextHash := s.fromHash
	nextHeight := s.fromHeight
	toFetch := s.blocksToFetch

	for toFetch > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Avoid network fetches for blocks already on disk, whether from the
		// node's own chain or an interrupted sync.
		if blk := rawdb.ReadBlock(s.db, nextHash, nextHeight); blk != nil {
			nextHash = blk.ParentHash()
			nextHeight--
			toFetch--
			continue
		}

		maxBlocks := uint16(min(toFetch, maxBlocksPerResponse))
		blocks, err := s.getBlocks(ctx, nextHash, nextHeight, maxBlocks)
		if err != nil {
			return fmt.Errorf("getting blocks at %d (%s): %w", nextHeight, nextHash, err)
		}

		batch := s.db.NewBatch()
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			nextHash = block.ParentHash()
			nextHeight--
			toFetch--
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("writing blocks after %d (%s): %w", nextHeight, nextHash, err)
		}
	}
	return nil
}

// getBlocks fetches the block with the given hash, followed by up to
// maxBlocks-1 of its ancestors, in descending height order. It keeps
// re-requesting from peers until a valid chain arrives or ctx ends.
func (s *Syncer) getBlocks(ctx context.Context, hash common.Hash, height uint64, maxBlocks uint16) ([]*types.Block, error) {
	req := &syncpb.GetBlockRequest{
		Height: height,
		// The field counts parents, so it excludes the block at height.
		NumParents: uint32(maxBlocks - 1),
	}
	var blocks []*types.Block
	_, err := s.client.Send(ctx, req,
		func(resp *syncpb.GetBlockResponse, nodeID ids.NodeID) error {
			b, err := verifyBlocks(hash, maxBlocks, resp.GetBlocks(), s.parseBlock)
			if err != nil {
				s.log.Debug("invalid block response, re-requesting",
					zap.Stringer("nodeID", nodeID),
					zap.Error(err),
				)
				return err
			}
			blocks = b
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

var (
	errNoBlocks            = errors.New("no blocks")
	errTooManyBlocks       = errors.New("too many blocks")
	errParsingBlock        = errors.New("parsing block")
	errUnexpectedBlockHash = errors.New("unexpected block hash")
)

// verifyBlocks parses blockBytes into a chain of blocks, verifying the first
// block has the given hash and the rest link through parent references.
func verifyBlocks(hash common.Hash, maxBlocks uint16, blockBytes [][]byte, parse Parser) ([]*types.Block, error) {
	if len(blockBytes) == 0 {
		return nil, errNoBlocks
	}
	if len(blockBytes) > int(maxBlocks) {
		return nil, fmt.Errorf("%w: got %d requested %d", errTooManyBlocks, len(blockBytes), maxBlocks)
	}

	blocks := make([]*types.Block, len(blockBytes))
	want := hash
	for i, bytes := range blockBytes {
		block, err := parse(bytes)
		if err != nil {
			return nil, fmt.Errorf("%w at index %d: %w", errParsingBlock, i, err)
		}
		if got := block.Hash(); got != want {
			return nil, fmt.Errorf("%w at index %d: got %s, expected %s", errUnexpectedBlockHash, i, got, want)
		}
		blocks[i] = block
		want = block.ParentHash()
	}
	return blocks, nil
}
