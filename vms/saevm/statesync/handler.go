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

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

// Config provides all user-configurable information for the [SummaryHandler].
type Config struct {
	CommitInterval uint64
	Enabled        *bool // nil means state sync is enabled only if no blocks have been accepted yet
}

var _ adaptor.SyncableVM[*Summary] = (*SummaryHandler)(nil)

// SummaryHandler implements [adaptor.SyncableVM] and provides the consensus-
// critical block getters for [adaptor.ChainVM].
type SummaryHandler struct {
	cfg Config
	db  ethdb.Database
	log logging.Logger

	stateSyncDone chan struct{}
}

// New constructs a new [SummaryHandler] with the given configuration and
// database.
func New(
	cfg Config,
	db ethdb.Database,
	log logging.Logger,
) (*SummaryHandler, error) {
	return &SummaryHandler{
		cfg:           cfg,
		db:            db,
		log:           log,
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
	hash, ok := h.lastAcceptedHash()
	if !ok {
		return h.GetStateSummary(ctx, 0)
	}

	lastHeight := rawdb.ReadHeaderNumber(h.db, hash)
	if lastHeight == nil {
		// This indicates a database inconsistency, can be considered fatal
		return nil, fmt.Errorf("%w: header not found for %s", database.ErrNotFound, hash)
	}

	height := saedb.LastCommittedTrieDBHeight(*lastHeight, h.cfg.CommitInterval)
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
	if height%h.cfg.CommitInterval != 0 {
		// can't serve committed state at this height
		return nil, database.ErrNotFound
	}

	id, err := h.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return nil, err
	}
	return NewSummary(common.Hash(id), height), nil
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
		return nil, fmt.Errorf("%w: block exists but not on disk %s:%d", database.ErrNotFound, id, *height)
	}

	return blocks.New(ethB, nil, nil, h.log)
}

// LastAccepted returns the ID of the last accepted block. If no blocks have
// been accepted, it returns the ID of the genesis block.
func (h *SummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	hash, ok := h.lastAcceptedHash()
	if !ok {
		return ids.Empty, database.ErrNotFound
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

// lastAcceptedHash returns the hash of the last accepted block.
// If not found, use in-memory genesis.
func (h *SummaryHandler) lastAcceptedHash() (common.Hash, bool) {
	hash := rawdb.ReadHeadFastBlockHash(h.db)
	if hash == (common.Hash{}) {
		return common.Hash{}, false
	}
	return hash, true
}
