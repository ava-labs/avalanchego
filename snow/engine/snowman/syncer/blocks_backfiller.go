// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ common.AncestorsHandler = (*BlockBackfiller)(nil)

	ErrNoPeersToDownloadBlocksFrom = errors.New("no connected peers to download blocks from")
)

type BlockBackfillerConfig struct {
	Ctx                            *snow.ConsensusContext
	VM                             block.ChainVM
	Sender                         common.Sender
	AncestorsMaxContainersSent     int
	AncestorsMaxContainersReceived int

	// BlockBackfiller is supposed to be embedded into the engine.
	// So requestID is shared among BlockBackfiller and the engine to
	// avoid duplications.
	SharedRequestID *uint32
}

type BlockBackfiller struct {
	BlockBackfillerConfig

	peers               sync.PeerTracker
	requestTimes        map[uint32]time.Time // requestID --> time of request issuance. Used to track bandwidth
	outstandingRequests common.Requests      // tracks which validators were asked for which block in which requests
	interrupted         bool                 // flag to allow backfilling restart after recovering from validators disconnections
}

func NewBlockBackfiller(cfg BlockBackfillerConfig, vals validators.Manager) (*BlockBackfiller, error) {
	pt, err := sync.NewPeerTracker(cfg.Ctx.Log, "", cfg.Ctx.Registerer)
	if err != nil {
		return nil, err
	}

	// TODO: pt must be initialized with the peers already connected, as well as their version
	valList := vals.GetValidatorIDs(cfg.Ctx.SubnetID)
	for _, val := range valList {
		pt.Connected(val, version.MinimumCompatibleVersion) // fake version, find a way to fix it
	}

	return &BlockBackfiller{
		BlockBackfillerConfig: cfg,

		peers:        pt,
		requestTimes: make(map[uint32]time.Time),
		interrupted:  pt.Size() == 0,
	}, nil
}

func (bb *BlockBackfiller) Start(ctx context.Context) error {
	ssVM, ok := bb.VM.(block.StateSyncableVM)
	if !ok {
		bb.Ctx.StateSyncing.Set(false)
		return nil // nothing to do
	}

	switch wantedBlk, _, err := ssVM.BackfillBlocksEnabled(ctx); {
	case errors.Is(err, block.ErrBlockBackfillingNotEnabled):
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
		bb.peers.TrackBandwidth(nodeID, 0)
		delete(bb.requestTimes, requestID)

		// Send another request for this
		return bb.fetch(ctx, wantedBlkID)
	}

	// track bandwidth
	responseSize := 0
	for _, blkBytes := range blks {
		responseSize += len(blkBytes)
	}
	bandwidth := sync.EstimateBandwidth(responseSize, bb.requestTimes[requestID])
	bb.peers.TrackBandwidth(nodeID, bandwidth)
	delete(bb.requestTimes, requestID)

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
	case errors.Is(err, block.ErrStopBlockBackfilling):
		bb.Ctx.Log.Info("block backfilling done")
		bb.Ctx.StateSyncing.Set(false)
		return nil
	case errors.Is(err, block.ErrInternalBlockBackfilling):
		bb.Ctx.Log.Debug("internal error while backfilling blocks",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		return err
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
	bb.peers.TrackBandwidth(nodeID, 0)
	delete(bb.requestTimes, requestID)

	// Send another request for this
	return bb.fetch(ctx, blkID)
}

// Get block [blkID] and its ancestors from a peer
func (bb *BlockBackfiller) fetch(ctx context.Context, blkID ids.ID) error {
	peerID, found := bb.peers.GetAnyPeer(version.MinimumCompatibleVersion)
	if !found {
		bb.interrupted = true
		return fmt.Errorf("dropping request for %s: %w", blkID, ErrNoPeersToDownloadBlocksFrom)
	}

	*bb.SharedRequestID++
	bb.outstandingRequests.Add(peerID, *bb.SharedRequestID, blkID)
	bb.Sender.SendGetAncestors(ctx, peerID, *bb.SharedRequestID, blkID)
	bb.requestTimes[*bb.SharedRequestID] = time.Now()
	return nil
}

func (bb *BlockBackfiller) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	bb.peers.Connected(nodeID, nodeVersion)
	if !bb.interrupted {
		return nil
	}

	// first validator reconnected. Resume blocks backfilling if needed
	bb.interrupted = false
	return bb.Start(ctx)
}

func (bb *BlockBackfiller) Disconnected(nodeID ids.NodeID) error {
	bb.peers.Disconnected(nodeID)
	bb.interrupted = (bb.peers.Size() == 0)
	return nil
}
