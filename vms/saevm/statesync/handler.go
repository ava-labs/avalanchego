// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/orchestrator"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/types"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var _ orchestrator.SummaryHandler[*Summary] = (*SummaryHandler)(nil)

type SummaryHandler struct {
	cfg     orchestrator.StateSyncConfig
	db      ethdb.Database
	snowCtx *snow.Context
	genesis *ethtypes.Block

	stateSyncDone chan struct{}
}

func (h *SummaryHandler) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	cfg orchestrator.StateSyncConfig,
	db database.Database,
	genesis *ethtypes.Block,
) error {
	h.cfg = cfg
	h.db = types.NewEthDB(db)
	h.snowCtx = snowCtx
	h.genesis = genesis
	h.stateSyncDone = make(chan struct{})
	return nil
}

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

	// TODO(alarso16): Do we need to handle the last synchronous block here?
	height := saedb.LastCommittedTrieDBHeight(*lastHeight, h.cfg.CommitInterval)
	return h.GetStateSummary(ctx, height)
}

// GetOngoingSyncStateSummary always returns [database.ErrNotFound].
// TODO(alarso16): track ongoing sync summary to allow resume
func (h *SummaryHandler) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
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
	return &Summary{
		height:    height,
		blockHash: common.Hash(id),
	}, nil
}

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

func (h *SummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	hash, ok := h.lastAcceptedHash()
	if !ok {
		hash = h.genesis.Hash()
	}
	return ids.ID(hash), nil
}

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
