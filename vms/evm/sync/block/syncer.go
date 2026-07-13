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
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/types"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	evmtypes "github.com/ava-labs/libevm/core/types"
)

var (
	_ types.Syncer = (*Syncer)(nil)

	errBlocksToFetchRequired   = errors.New("blocksToFetch must be greater than zero")
	errFromHashRequired        = errors.New("fromHash must be non-zero when fromHeight is greater than zero")
	errEmptyResponse           = errors.New("empty block response")
	errTooManyBlocks           = errors.New("more blocks returned than requested")
	errDecodeBlock             = errors.New("failed to decode block")
	errBlockHashMismatch       = errors.New("block does not hash to the expected value")
	errTxHashMismatch          = errors.New("transactions do not hash to the header value")
	errUncleHashMismatch       = errors.New("uncles do not hash to the header value")
	errWithdrawalsHashMismatch = errors.New("withdrawals do not hash to the header value")
	errMissingWithdrawals      = errors.New("header commits to withdrawals but the body has none")
	errUnexpectedWithdrawals   = errors.New("body has withdrawals but the header commits to none")
)

// BlockVerifier rejects a block for chain rules this package cannot know.
type BlockVerifier func(*evmtypes.Block) error

type syncerConfig struct {
	verifyBlock BlockVerifier
}

// SyncerOption configures a [Syncer] at construction time.
type SyncerOption = options.Option[syncerConfig]

// WithBlockVerifier adds a chain-specific check, such as C-Chain ExtDataHash.
func WithBlockVerifier(v BlockVerifier) SyncerOption {
	return options.Func[syncerConfig](func(c *syncerConfig) {
		c.verifyBlock = v
	})
}

// Syncer fetches a contiguous run of blocks by walking parents from a known tip
// and writes them to db. It skips any suffix already on disk, verifies every
// response links tip-to-parent, and re-requests on failure.
type Syncer struct {
	log           logging.Logger
	client        *Client
	db            ethdb.Database
	fromHash      common.Hash
	fromHeight    uint64
	blocksToFetch uint64
	verifyBlock   BlockVerifier // nil when the caller supplied no chain-specific check
}

// NewSyncer returns a [Syncer] that fetches blocksToFetch blocks ending at
// (fromHash, fromHeight) and writes them into db, fetching from peers through c.
func NewSyncer(log logging.Logger, c *Client, db ethdb.Database, fromHash common.Hash, fromHeight, blocksToFetch uint64, opts ...SyncerOption) (*Syncer, error) {
	if blocksToFetch == 0 {
		return nil, errBlocksToFetchRequired
	}
	if (fromHash == common.Hash{}) && fromHeight > 0 {
		return nil, errFromHashRequired
	}

	var cfg syncerConfig
	options.ApplyTo(&cfg, opts...)

	return &Syncer{
		log:           log,
		client:        c,
		db:            db,
		fromHash:      fromHash,
		fromHeight:    fromHeight,
		blocksToFetch: blocksToFetch,
		verifyBlock:   cfg.verifyBlock,
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

	// Skip any suffix already on disk, whether from the node's own chain or an
	// interrupted sync.
	for toFetch > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
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

		parents := uint16(min(toFetch, uint64(maxParentsPerRequest)))
		blocks, err := getBlocks(ctx, s.log, s.client, nextHash, nextHeight, parents, s.verifyBlock)
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

		if batch.ValueSize() < ethdb.IdealBatchSize {
			continue
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("could not write blocks at %s: %w", nextHash, err)
		}
		batch.Reset()
	}

	return batch.Write()
}

// getBlocks requests up to numParents blocks ending at (hash, height), verifies
// the returned chain links back from hash, scores the peer, and re-requests on
// any network or verification failure until ctx ends.
func getBlocks(ctx context.Context, log logging.Logger, c *Client, hash common.Hash, height uint64, numParents uint16, verify BlockVerifier) ([]*evmtypes.Block, error) {
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

		blocks, err := verifyBlocks(hash, numParents, resp.GetBlocks(), verify)
		if err != nil {
			outcome.Failure()
			log.Debug("invalid block response, re-requesting", zap.Error(err))
			continue
		}

		outcome.Success()
		return blocks, nil
	}
}

// verifyBlocks decodes raw and reports whether it is the parent chain ending at
// hash, in tip-first order.
func verifyBlocks(hash common.Hash, numParents uint16, raw [][]byte, verify BlockVerifier) ([]*evmtypes.Block, error) {
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
		if err := verifyBody(block); err != nil {
			return nil, fmt.Errorf("at index %d: %w", i, err)
		}
		if verify != nil {
			if err := verify(block); err != nil {
				return nil, fmt.Errorf("at index %d: %w", i, err)
			}
		}
		blocks[i] = block
		want = block.ParentHash()
	}
	return blocks, nil
}

// verifyBody matches the body against the header roots. The block hash covers
// the header alone, so decoding accepts any body until these are recomputed.
func verifyBody(block *evmtypes.Block) error {
	if got := evmtypes.CalcUncleHash(block.Uncles()); got != block.UncleHash() {
		return fmt.Errorf("%w: got %s expected %s", errUncleHashMismatch, got, block.UncleHash())
	}
	if got := evmtypes.DeriveSha(block.Transactions(), trie.NewStackTrie(nil)); got != block.TxHash() {
		return fmt.Errorf("%w: got %s expected %s", errTxHashMismatch, got, block.TxHash())
	}

	wantWithdrawals := block.Header().WithdrawalsHash
	switch {
	case wantWithdrawals != nil:
		if block.Withdrawals() == nil {
			return errMissingWithdrawals
		}
		if got := evmtypes.DeriveSha(block.Withdrawals(), trie.NewStackTrie(nil)); got != *wantWithdrawals {
			return fmt.Errorf("%w: got %s expected %s", errWithdrawalsHashMismatch, got, *wantWithdrawals)
		}
	case block.Withdrawals() != nil:
		return errUnexpectedWithdrawals
	}
	return nil
}
