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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	syncblock "github.com/ava-labs/avalanchego/vms/evm/sync/block"
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// Config provides all user-configurable information for the [SummaryHandler].
type Config struct {
	CommitInterval         uint64
	Enabled                *bool
	ExtraBlockVerification syncblock.BlockVerifier
}

var _ adaptor.SyncableVM[*Summary] = (*SummaryHandler)(nil)

// SummaryHandler implements [adaptor.SyncableVM] and provides the consensus-
// critical block getters for [adaptor.ChainVM].
type SummaryHandler struct {
	cfg        Config
	db         ethdb.Database
	snowCtx    *snow.Context
	genesis    *ethtypes.Block
	network    *network.Network
	hooks      hook.Points
	cancelSync context.CancelFunc

	stateSyncDone chan struct{}
	// syncErr is set by the [AcceptSummary] goroutine before stateSyncDone is
	// closed, and read by [SummaryHandler.Error] after observing that close.
	syncErr error
}

// New constructs a new [SummaryHandler] with the given configuration and
// database. The genesis block must be provided to allow the handler to return
// the summary of the genesis block.
func New(
	cfg Config,
	snowCtx *snow.Context,
	db ethdb.Database,
	genesis *ethtypes.Block,
	network *network.Network,
	hooks hook.Points,
) (*SummaryHandler, error) {
	h := &SummaryHandler{
		cfg:           cfg,
		db:            db,
		snowCtx:       snowCtx,
		network:       network,
		genesis:       genesis,
		hooks:         hooks,
		stateSyncDone: make(chan struct{}),
	}
	return h, nil
}

// Shutdown cancels any ongoing sync.
func (h *SummaryHandler) Shutdown(ctx context.Context) error {
	if h.cancelSync == nil {
		// no sync was ever started
		return nil
	}
	h.cancelSync()

	select {
	case <-h.stateSyncDone:
	case <-ctx.Done():
		return ctx.Err()
	}

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

	// TODO(alarso16): Do we need to handle the last synchronous block here?
	height := saedb.LastCommittedTrieDBHeight(*lastHeight, h.cfg.CommitInterval)
	return h.GetStateSummary(ctx, height)
}

// GetOngoingSyncStateSummary always returns [database.ErrNotFound].
// TODO(alarso16): track ongoing sync summary to allow resume
func (*SummaryHandler) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
	return nil, database.ErrNotFound
}

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
	var ethB *ethtypes.Block
	if id == ids.ID(h.genesis.Hash()) {
		ethB = h.genesis
	} else {
		height := rawdb.ReadHeaderNumber(h.db, common.Hash(id))
		if height == nil {
			return nil, database.ErrNotFound
		}
		ethB = rawdb.ReadBlock(h.db, common.Hash(id), *height)
		if ethB == nil {
			// This indicates a database inconsistency, so we don't need to return [database.ErrNotFound] directly.
			return nil, fmt.Errorf("%w: block not found for %s:%d", database.ErrNotFound, id, *height)
		}
	}

	return blocks.New(ethB, nil, nil, h.snowCtx.Log)
}

// LastAccepted returns the ID of the last accepted block. If no blocks have
// been accepted, it returns the ID of the genesis block.
func (h *SummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	hash, ok := h.lastAcceptedHash()
	if !ok {
		hash = h.genesis.Hash()
	}
	return ids.ID(hash), nil
}

// GetBlockIDAtHeight returns the ID of the block at the given height. If no
// block exists at that height, it returns [database.ErrNotFound].
func (h *SummaryHandler) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	if height == 0 {
		return ids.ID(h.genesis.Hash()), nil
	}
	hash := rawdb.ReadCanonicalHash(h.db, height)
	if hash == (common.Hash{}) {
		return ids.ID{}, database.ErrNotFound
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
