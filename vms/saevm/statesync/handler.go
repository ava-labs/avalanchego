// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package statesync provides all functionality required for an
// [adaptor.SyncableVM] and the consensus-critical block getters.
package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	syncblocks "github.com/ava-labs/avalanchego/vms/evm/sync/block"
)

// Config provides all user-configurable information for the [Handler].
type Config struct {
	DBConfig saedb.Config
	Enabled  bool
}

// Handler implements provides server-side [Summary] handling and parsing, as
// well as providing critical block getters for a ChainVM.
type Handler struct {
	cfg         Config
	db          ethdb.Database
	hooks       hook.Points
	snowCtx     *snow.Context
	network     *network.Network
	blockParser syncblocks.Parser
}

// New constructs a new [Handler] with the given configuration and
// database. See the README for the guarantees expected of the database.
func New(
	cfg Config,
	db ethdb.Database,
	snowCtx *snow.Context,
	network *network.Network,
	hooks hook.Points,
) (*Handler, error) {
	if err := cfg.DBConfig.Verify(); err != nil {
		return nil, err
	}
	return &Handler{
		cfg:         cfg,
		db:          db,
		snowCtx:     snowCtx,
		network:     network,
		hooks:       hooks,
		blockParser: parser(hooks),
	}, nil
}

// parser returns a [syncblocks.Parser] that uses the given hooks to parse blocks.
func parser(hooks hook.Points) syncblocks.Parser {
	return func(blkBytes []byte) (*types.Block, error) {
		return blocks.ParseEth(blkBytes, hooks)
	}
}

// GetLastStateSummary returns the summary of the last accepted block at
// multiple of [syncCommitInterval] height.
func (h *Handler) GetLastStateSummary(context.Context) (*Summary, error) {
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return nil, err
	}

	lastHeight := rawdb.ReadHeaderNumber(h.db, hash)
	if lastHeight == nil {
		// This indicates a database inconsistency, can be considered fatal
		err := fmt.Errorf("%w: header not found for %s", database.ErrNotFound, hash)
		h.snowCtx.Log.Warn("rawdb.ReadHeaderNumber in GetLastStateSummary", zap.Error(err))
		return nil, err
	}

	height := saedb.LastCommittedTrieDBHeight(*lastHeight, h.cfg.DBConfig.CommitInterval)
	return h.getSummaryAtHeight(height)
}

// GetStateSummary returns the summary of the block at the given height, if it
// is available to be served. Otherwise, [database.ErrNotFound] is returned.
func (h *Handler) GetStateSummary(ctx context.Context, height uint64) (*Summary, error) {
	if !saedb.ShouldCommitTrieDB(height, h.cfg.DBConfig.CommitInterval) {
		// can't serve committed state at this height
		return nil, database.ErrNotFound
	}
	return h.getSummaryAtHeight(height)
}

func (h *Handler) getSummaryAtHeight(height uint64) (*Summary, error) {
	hash, err := h.getHashAtHeight(height)
	if err != nil {
		return nil, err
	}

	hdr := rawdb.ReadHeader(h.db, hash, height)
	if hdr == nil {
		h.snowCtx.Log.Warn("rawdb.ReadHeader in getSummaryAtHeight",
			zap.Stringer("hash", hash),
			zap.Uint64("height", height),
		)
		return nil, database.ErrNotFound
	}

	// State sync will not work with synchronous blocks.
	if hook.Synchronous(h.hooks, hdr) {
		return nil, database.ErrNotFound
	}

	return NewSummary(hash, height), nil
}

// ParseBlock parses the given bytes into a [blocks.Block] via [blocks.ParseEth]
// if it is well-formed. Any returned block is safe to be used after state sync
// finishes.
func (h *Handler) ParseBlock(_ context.Context, blkBytes []byte) (*blocks.Block, error) {
	ethB, err := h.blockParser(blkBytes)
	if err != nil {
		return nil, err
	}
	return blocks.New(ethB, nil, nil, h.hooks, h.snowCtx.Log)
}

// GetBlock returns the block with the given ID. If the block is not found, it
// returns [database.ErrNotFound].
func (h *Handler) GetBlock(_ context.Context, id ids.ID) (*blocks.Block, error) {
	height := rawdb.ReadHeaderNumber(h.db, common.Hash(id))
	if height == nil {
		return nil, database.ErrNotFound
	}
	ethB := rawdb.ReadBlock(h.db, common.Hash(id), *height)
	if ethB == nil {
		// This indicates a database inconsistency, so we don't need to return [database.ErrNotFound] directly.
		err := fmt.Errorf("%w: block not found %s:%d", database.ErrNotFound, id, *height)
		h.snowCtx.Log.Warn("rawdb.ReadBlock in GetBlock", zap.Error(err))
		return nil, err
	}

	return blocks.New(ethB, nil, nil, h.hooks, h.snowCtx.Log)
}

// LastAccepted returns the ID of the last accepted block. If no blocks have
// been accepted, it returns the ID of the genesis block.
func (h *Handler) LastAccepted(context.Context) (ids.ID, error) {
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return ids.Empty, err
	}
	return ids.ID(hash), nil
}

// GetBlockIDAtHeight returns the ID of the block at the given height. If no
// block exists at that height, it returns [database.ErrNotFound].
func (h *Handler) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	hash, err := h.getHashAtHeight(height)
	return ids.ID(hash), err
}

func (h *Handler) getHashAtHeight(height uint64) (common.Hash, error) {
	hash := rawdb.ReadCanonicalHash(h.db, height)
	if hash == (common.Hash{}) {
		return common.Hash{}, database.ErrNotFound
	}
	return hash, nil
}

// lastAcceptedHash returns the hash of the last accepted block, and whether
// one exists.
func (h *Handler) lastAcceptedHash() (common.Hash, error) {
	// The database is guaranteed to have this populated.
	hash := rawdb.ReadHeadFastBlockHash(h.db)
	if hash == (common.Hash{}) {
		h.snowCtx.Log.Warn("rawdb.ReadHeadFastBlockHash returned empty")
		return common.Hash{}, database.ErrNotFound
	}
	return hash, nil
}
