// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ common.AncestorsHandler = (*BlockBackfiller)(nil)

	ErrNoPeersToDownloadBlocksFrom = errors.New("no connected peers to download blocks from")
)

type BlockBackfillerConfig struct {
	Ctx                            *snow.ConsensusContext
	VM                             block.ChainVM
	Sender                         common.Sender
	Validators                     validators.Manager
	Peers                          tracker.Peers
	AncestorsMaxContainersSent     int
	AncestorsMaxContainersReceived int

	// BlockBackfiller is supposed to be embedded into the engine.
	// So requestID is shared among BlockBackfiller and the engine to
	// avoid duplications.
	SharedRequestID *uint32
}

type BlockBackfiller struct {
	BlockBackfillerConfig

	fetchFrom           set.Set[ids.NodeID] // picked from bootstrapper
	outstandingRequests common.Requests     // tracks which validators were asked for which block in which requests
	interrupted         bool                // flag to allow backfilling restart after recovering from validators disconnections
}

func NewBlockBackfiller(cfg BlockBackfillerConfig) *BlockBackfiller {
	return &BlockBackfiller{
		BlockBackfillerConfig: cfg,

		fetchFrom:   set.Of[ids.NodeID](cfg.Validators.GetValidatorIDs(cfg.Ctx.SubnetID)...),
		interrupted: len(cfg.Peers.PreferredPeers()) > 0,
	}
}

func (bb *BlockBackfiller) Start(ctx context.Context) error {
	ssVM, ok := bb.VM.(block.StateSyncableVM)
	if !ok {
		bb.Ctx.StateSyncing.Set(false)
		return nil // nothing to do
	}

	switch wantedBlk, _, err := ssVM.BackfillBlocksEnabled(ctx); {
	case err == block.ErrBlockBackfillingNotEnabled:
		bb.Ctx.Log.Info("block backfilling not enabled")
		bb.Ctx.StateSyncing.Set(false)
		return nil
	case err != nil:
		return fmt.Errorf("failed checking if state sync block backfilling is enabled: %w", err)
	default:
		return bb.fetch(ctx, wantedBlk)
	}
}

// Ancestors handles the receipt of multiple containers. Should be received in
// response to a GetAncestors message to [nodeID] with request ID [requestID]
func (bb *BlockBackfiller) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, blks [][]byte) error {
	// Make sure this is in response to a request we made
	wantedBlkID, ok := bb.outstandingRequests.Remove(nodeID, requestID)
	if !ok { // this message isn't in response to a request we made
		bb.Ctx.Log.Debug("received unexpected Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	lenBlks := len(blks)
	if lenBlks == 0 {
		bb.Ctx.Log.Debug("received Ancestors with no block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)

		bb.markUnavailable(nodeID)

		// Send another request for this
		return bb.fetch(ctx, wantedBlkID)
	}

	// This node has responded - so add it back into the set
	bb.fetchFrom.Add(nodeID)

	if lenBlks > bb.AncestorsMaxContainersReceived {
		blks = blks[:bb.AncestorsMaxContainersReceived]
		bb.Ctx.Log.Debug("ignoring containers in Ancestors",
			zap.Int("numContainers", lenBlks-bb.AncestorsMaxContainersReceived),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}

	ssVM, ok := bb.VM.(block.StateSyncableVM)
	if !ok {
		return nil // nothing to do
	}

	switch nextWantedBlkID, _, err := ssVM.BackfillBlocks(ctx, blks); {
	case err == block.ErrStopBlockBackfilling:
		bb.Ctx.Log.Info("block backfilling done")
		bb.Ctx.StateSyncing.Set(false)
		return nil
	case err != nil:
		bb.Ctx.Log.Debug("failed to backfill blocks in Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		return bb.fetch(ctx, wantedBlkID)
	default:
		return bb.fetch(ctx, nextWantedBlkID)
	}
}

func (bb *BlockBackfiller) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	blkID, ok := bb.outstandingRequests.Remove(nodeID, requestID)
	if !ok {
		bb.Ctx.Log.Debug("unexpectedly called GetAncestorsFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// This node timed out their request, so we can add them back to [fetchFrom]
	bb.fetchFrom.Add(nodeID)

	// Send another request for this
	return bb.fetch(ctx, blkID)
}

// Get block [blkID] and its ancestors from a peer
func (bb *BlockBackfiller) fetch(ctx context.Context, blkID ids.ID) error {
	validatorID, ok := bb.fetchFrom.Peek()
	if !ok {
		return fmt.Errorf("dropping request for %s: %w", blkID, ErrNoPeersToDownloadBlocksFrom)
	}

	// We only allow one outbound request at a time from a node
	bb.markUnavailable(validatorID)
	*bb.SharedRequestID++
	bb.outstandingRequests.Add(validatorID, *bb.SharedRequestID, blkID)
	bb.Sender.SendGetAncestors(ctx, validatorID, *bb.SharedRequestID, blkID)
	return nil
}

func (bb *BlockBackfiller) markUnavailable(nodeID ids.NodeID) {
	bb.fetchFrom.Remove(nodeID)

	// if [fetchFrom] has become empty, reset it to the currently preferred
	// peers
	if bb.fetchFrom.Len() == 0 {
		bb.fetchFrom = bb.Peers.PreferredPeers()
	}
}

func (bb *BlockBackfiller) Connected(ctx context.Context, nodeID ids.NodeID) error {
	// Ensure fetchFrom reflects proper validator list
	if _, ok := bb.Validators.GetValidator(bb.Ctx.SubnetID, nodeID); ok {
		bb.fetchFrom.Add(nodeID)
	}

	if !bb.interrupted {
		return nil
	}

	// first validator reconnected. Resume blocks backfilling if needed
	bb.interrupted = false
	return bb.Start(ctx)
}

func (bb *BlockBackfiller) Disconnected(nodeID ids.NodeID) error {
	bb.markUnavailable(nodeID)

	// if there is no validator left, flag that blocks backfilling is interrupted.
	bb.interrupted = len(bb.fetchFrom) == 0
	return nil
}
