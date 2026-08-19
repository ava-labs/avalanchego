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
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

// Config provides all user-configurable information for the [SummaryHandler].
type Config struct {
	DBConfig saedb.Config
	Enabled  bool
}

var _ adaptor.SyncableVM[*Summary] = (*SummaryHandler)(nil)

// SummaryHandler implements [adaptor.SyncableVM] and provides the consensus-
// critical block getters for [adaptor.ChainVM].
type SummaryHandler struct {
	cfg   Config
	db    ethdb.Database
	log   logging.Logger
	hooks hook.Points

	stateSyncDone chan struct{}
}

// New constructs a new [SummaryHandler] with the given configuration and
// database. See the README for the guarantees expected of the database.
func New(
	cfg Config,
	db ethdb.Database,
	log logging.Logger,
	hooks hook.Points,
) (*SummaryHandler, error) {
	if err := cfg.DBConfig.Verify(); err != nil {
		return nil, err
	}
	return &SummaryHandler{
		cfg:           cfg,
		db:            db,
		log:           log,
		hooks:         hooks,
		stateSyncDone: make(chan struct{}),
	}, nil
}

// Shutdown cancels any ongoing sync.
func (*SummaryHandler) Shutdown(context.Context) error {
	// TODO(alarso16): cancel any ongoing state sync
	return nil
}

// GetLastStateSummary returns the summary of the last accepted block at
// multiple of [syncCommitInterval] height.
func (h *SummaryHandler) GetLastStateSummary(ctx context.Context) (*Summary, error) {
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return nil, err
	}

	lastHeight := rawdb.ReadHeaderNumber(h.db, hash)
	if lastHeight == nil {
		// This indicates a database inconsistency, can be considered fatal
		err := fmt.Errorf("%w: header not found for %s", database.ErrNotFound, hash)
		h.log.Warn("rawdb.ReadHeaderNumber in GetLastStateSummary", zap.Error(err))
		return nil, err
	}

	height := saedb.LastCommittedTrieDBHeight(*lastHeight, h.cfg.DBConfig.CommitInterval)
	return h.GetStateSummary(ctx, height)
}

// GetOngoingSyncStateSummary always returns [database.ErrNotFound].
// TODO(alarso16): track ongoing sync summary to allow resume
func (*SummaryHandler) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
	return nil, database.ErrNotFound
}

// GetStateSummary returns the summary of the block at the given height, if it
// is available to be served. Otherwise, [database.ErrNotFound] is returned.
//
// TODO(alarso16): don't serve summaries for synchronous blocks.
func (h *SummaryHandler) GetStateSummary(ctx context.Context, height uint64) (*Summary, error) {
	if !saedb.ShouldCommitTrieDB(height, h.cfg.DBConfig.CommitInterval) {
		// can't serve committed state at this height
		return nil, database.ErrNotFound
	}

	id, err := h.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return nil, err
	}
	return NewSummary(common.Hash(id), height), nil
}

// ParseBlock parses the given bytes into a [blocks.Block] via [blocks.ParseEth]
// if it is well-formed. Any returned block is safe to be used after state sync
// finishes.
func (h *SummaryHandler) ParseBlock(_ context.Context, blkBytes []byte) (*blocks.Block, error) {
	ethB, err := blocks.ParseEth(blkBytes, h.hooks)
	if err != nil {
		return nil, err
	}
	return blocks.New(ethB, nil, nil, h.log)
}

// GetBlock returns the block with the given ID. If the block is not found, it
// returns [database.ErrNotFound].
func (h *SummaryHandler) GetBlock(_ context.Context, id ids.ID) (*blocks.Block, error) {
	height := rawdb.ReadHeaderNumber(h.db, common.Hash(id))
	if height == nil {
		return nil, database.ErrNotFound
	}
	ethB := rawdb.ReadBlock(h.db, common.Hash(id), *height)
	if ethB == nil {
		// This indicates a database inconsistency, so we don't need to return [database.ErrNotFound] directly.
		err := fmt.Errorf("%w: block not found %s:%d", database.ErrNotFound, id, *height)
		h.log.Warn("rawdb.ReadBlock in GetBlock", zap.Error(err))
		return nil, err
	}

	return blocks.New(ethB, nil, nil, h.log)
}

// LastAccepted returns the ID of the last accepted block. If no blocks have
// been accepted, it returns the ID of the genesis block.
func (h *SummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return ids.Empty, err
	}
	return ids.ID(hash), nil
}

// GetBlockIDAtHeight returns the ID of the block at the given height. If no
// block exists at that height, it returns [database.ErrNotFound].
func (h *SummaryHandler) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	hash := rawdb.ReadCanonicalHash(h.db, height)
	if hash == (common.Hash{}) {
		return ids.Empty, database.ErrNotFound
	}
	return ids.ID(hash), nil
}

// lastAcceptedHash returns the hash of the last accepted block, and whether
// one exists.
func (h *SummaryHandler) lastAcceptedHash() (common.Hash, error) {
	// The database is guaranteed to have this populated.
	hash := rawdb.ReadHeadFastBlockHash(h.db)
	if hash == (common.Hash{}) {
		h.log.Warn("rawdb.ReadHeadFastBlockHash returned empty")
		return common.Hash{}, database.ErrNotFound
	}
	return hash, nil
}
