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

	"github.com/ava-labs/avalanchego/utils/logging"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	errBlocksToFetchRequired = errors.New("blocksToFetch must be greater than zero")
	errFromHashRequired      = errors.New("fromHash must be non-zero when fromHeight is greater than zero")
	errParseBlockRequired    = errors.New("parseBlock must be non-nil")
	errEmptyResponse         = errors.New("empty block response")
	errTooManyBlocks         = errors.New("more blocks returned than requested")
	errParseBlock            = errors.New("failed to parse block")
	errBlockHashMismatch     = errors.New("block does not hash to the expected value")
)

// BlockParser decodes a block and enforces every invariant it must satisfy,
// including the header roots.
type BlockParser func([]byte) (*types.Block, error)

// Syncer fetches a contiguous run of blocks by walking parents from a known tip
// and writes them to db. It skips blocks already on disk, verifies every
// response links tip-to-parent, and re-requests on failure.
type Syncer struct {
	log           logging.Logger
	client        *Client
	db            ethdb.Database
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64
	parseBlock    BlockParser
}

// NewSyncer returns a [Syncer] that walks back from (fromHash, fromHeight),
// which counts as the first of blocksToFetch.
func NewSyncer(log logging.Logger, c *Client, db ethdb.Database, parse BlockParser, fromHash common.Hash, fromHeight, blocksToFetch uint64) (*Syncer, error) {
	if blocksToFetch == 0 {
		return nil, errBlocksToFetchRequired
	}
	if (fromHash == common.Hash{}) && fromHeight > 0 {
		return nil, errFromHashRequired
	}
	if parse == nil {
		return nil, errParseBlockRequired
	}

	return &Syncer{
		log:           log,
		client:        c,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: blocksToFetch,
		parseBlock:    parse,
	}, nil
}

// Sync stops at blocksToFetch, at genesis, or when ctx ends. A chain shorter
// than blocksToFetch is not an error.
//
// Calling it again resumes from disk, so a cancelled sync can be retried on
// the same [Syncer].
func (s *Syncer) Sync(ctx context.Context) error {
	nextHash := s.fromHash
	nextHeight := s.fromHeight
	toFetch := s.blocksToFetch

	var fetchErr error
	for toFetch > 0 && nextHash != (common.Hash{}) {
		if err := ctx.Err(); err != nil {
			fetchErr = err
			break
		}

		// Skip anything already on disk, from the node's own chain or an
		// interrupted sync.
		if blk := rawdb.ReadBlock(s.db, nextHash, nextHeight); blk != nil {
			nextHash = blk.ParentHash()
			nextHeight--
			toFetch--
			continue
		}

		maxBlocks := uint16(min(toFetch, uint64(maxBlocksPerResponse)))
		blocks, err := getBlocks(ctx, s.log, s.client, nextHash, nextHeight, maxBlocks, s.parseBlock)
		if err != nil {
			fetchErr = fmt.Errorf("could not get blocks at %s: %w", nextHash, err)
			break
		}

		batch := s.db.NewBatch()
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
			nextHash = block.ParentHash()
			nextHeight--
			toFetch--
		}

		// Flushing each round keeps verified work on a restart. The response
		// is already bounded, so the batch cannot grow past one of them.
		if err := batch.Write(); err != nil {
			return fmt.Errorf("could not write blocks at %s: %w", nextHash, err)
		}
	}
	return fetchErr
}

// getBlocks requests up to maxBlocks blocks ending at (hash, height), verifies
// the returned chain links back from hash, scores the peer, and re-requests on
// any network or verification failure until ctx ends.
func getBlocks(ctx context.Context, log logging.Logger, c *Client, hash common.Hash, height uint64, maxBlocks uint16, parse BlockParser) ([]*types.Block, error) {
	req := &syncpb.GetBlockRequest{
		Height: height,
		// The field counts parents, so it excludes the block at height.
		NumParents: uint32(maxBlocks - 1),
	}
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

		blocks, err := verifyBlocks(hash, maxBlocks, resp.GetBlocks(), parse)
		if err != nil {
			outcome.Failure()
			log.Debug("invalid block response, re-requesting", zap.Error(err))
			continue
		}

		outcome.Success()
		return blocks, nil
	}
}

// verifyBlocks parses raw and reports whether it is the parent chain ending at
// hash, in tip-first order.
func verifyBlocks(hash common.Hash, maxBlocks uint16, raw [][]byte, parse BlockParser) ([]*types.Block, error) {
	if len(raw) == 0 {
		return nil, errEmptyResponse
	}
	if len(raw) > int(maxBlocks) {
		return nil, fmt.Errorf("%w: got %d requested %d", errTooManyBlocks, len(raw), maxBlocks)
	}

	blocks := make([]*types.Block, len(raw))
	want := hash
	for i, blockBytes := range raw {
		block, err := parse(blockBytes)
		if err != nil {
			return nil, fmt.Errorf("%w at index %d: %w", errParseBlock, i, err)
		}
		if got := block.Hash(); got != want {
			return nil, fmt.Errorf("%w at index %d: got %s expected %s", errBlockHashMismatch, i, got, want)
		}
		blocks[i] = block
		want = block.ParentHash()
	}
	return blocks, nil
}
