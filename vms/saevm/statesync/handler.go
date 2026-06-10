package statesync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

var _ adaptor.StateSyncable[*Summary] = (*SummaryHandler)(nil)

type SummaryHandler struct {
	cfg     Config
	db      ethdb.Database
	network sae.Network
	snowCtx *snow.Context

	stateSyncDone chan struct{}
}

type Config struct {
	StateSyncEnabled *bool
	StateSyncIDs     string
	StateScheme      string
}

func NewSummaryHandler(cfg Config, db ethdb.Database, network sae.Network, snowCtx *snow.Context) *SummaryHandler {
	return &SummaryHandler{
		cfg:           cfg,
		db:            db,
		network:       network,
		snowCtx:       snowCtx,
		stateSyncDone: make(chan struct{}),
	}
}

func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// AcceptSummary implements [adaptor.StateSyncable].
func (h *SummaryHandler) AcceptSummary(context.Context, *Summary) (block.StateSyncMode, error) {
	if h.cfg.StateSyncEnabled != nil && !*h.cfg.StateSyncEnabled {
		return block.StateSyncSkipped, nil
	}
	go func() {
		// TODO(alarso16): implement state sync
		close(h.stateSyncDone)
	}()

	return block.StateSyncStatic, nil
}

// GetLastStateSummary implements [adaptor.StateSyncable].
func (h *SummaryHandler) GetLastStateSummary(ctx context.Context) (*Summary, error) {
	lastHdr := rawdb.ReadHeadHeader(h.db)
	if lastHdr == nil {
		return nil, errors.New("no head header found")
	}

	// We must know the settled state for the last accepted height, otherwise
	// verification would have failed.
	height := saedb.LastCommittedTrieDBHeight(lastHdr.Number.Uint64(), syncCommitInterval)
	return h.GetStateSummary(ctx, height)
}

// GetOngoingSyncStateSummary implements [adaptor.StateSyncable].
func (h *SummaryHandler) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
	// TODO(alarso16): track ongoing sync summary
	// Unnecessary for now, we can just restart, who cares
	return nil, database.ErrNotFound
}

// GetStateSummary implements [adaptor.StateSyncable].
func (h *SummaryHandler) GetStateSummary(_ context.Context, height uint64) (*Summary, error) {
	hash := rawdb.ReadCanonicalHash(h.db, height)
	hdr := rawdb.ReadHeader(h.db, hash, height)
	if hdr == nil {
		return nil, fmt.Errorf("%w: block at height %d", database.ErrNotFound, height)
	}

	return &Summary{
		height:    height,
		blockHash: hash,
	}, nil
}

// StateSyncEnabled implements [adaptor.StateSyncable].
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	return true, nil
}
