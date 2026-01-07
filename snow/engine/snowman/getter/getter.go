// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Get requests are always served, regardless node state (bootstrapping or normal operations).
var _ common.AllGetsServer = (*getter)(nil)

func New(
	vm block.ChainVM,
	sender common.Sender,
	log logging.Logger,
	maxTimeGetAncestors time.Duration,
	maxContainersGetAncestors int,
	reg prometheus.Registerer,
) (common.AllGetsServer, error) {
	ssVM, _ := vm.(block.StateSyncableVM)
	gh := &getter{
		vm:                        vm,
		ssVM:                      ssVM,
		sender:                    sender,
		log:                       log,
		maxTimeGetAncestors:       maxTimeGetAncestors,
		maxContainersGetAncestors: maxContainersGetAncestors,
	}

	var err error
	gh.getAncestorsBlks, err = metric.NewAverager(
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		reg,
	)
	return gh, err
}

type getter struct {
	vm   block.ChainVM
	ssVM block.StateSyncableVM // can be nil

	sender common.Sender
	log    logging.Logger
	// Max time to spend fetching a container and its ancestors when responding
	// to a GetAncestors
	maxTimeGetAncestors time.Duration
	// Max number of containers in an ancestors message sent by this node.
	maxContainersGetAncestors int

	getAncestorsBlks metric.Averager
}

func (gh *getter) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// Note: we do not check if gh.ssVM.StateSyncEnabled since we want all
	// nodes, including those disabling state sync to serve state summaries if
	// these are available
	if gh.ssVM == nil {
		gh.log.Debug("dropping GetStateSummaryFrontier message",
			zap.String("reason", "state sync not supported"),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	summary, err := gh.ssVM.GetLastStateSummary(ctx)
	if err != nil {
		gh.log.Debug("dropping GetStateSummaryFrontier message",
			zap.String("reason", "couldn't get state summary frontier"),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		return nil
	}

	gh.sender.SendStateSummaryFrontier(ctx, nodeID, requestID, summary.Bytes())
	return nil
}

func (gh *getter) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights set.Set[uint64]) error {
	// If there are no requested heights, then we can return the result
	// immediately, regardless of if the underlying VM implements state sync.
	if heights.Len() == 0 {
		gh.sender.SendAcceptedStateSummary(ctx, nodeID, requestID, nil)
		return nil
	}

	// Note: we do not check if gh.ssVM.StateSyncEnabled since we want all
	// nodes, including those disabling state sync to serve state summaries if
	// these are available
	if gh.ssVM == nil {
		gh.log.Debug("dropping GetAcceptedStateSummary message",
			zap.String("reason", "state sync not supported"),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	summaryIDs := make([]ids.ID, 0, heights.Len())
	for height := range heights {
		summary, err := gh.ssVM.GetStateSummary(ctx, height)
		if err == block.ErrStateSyncableVMNotImplemented {
			gh.log.Debug("dropping GetAcceptedStateSummary message",
				zap.String("reason", "state sync not supported"),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
			return nil
		}
		if err != nil {
			gh.log.Debug("couldn't get state summary",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			continue
		}
		summaryIDs = append(summaryIDs, summary.ID())
	}

	gh.sender.SendAcceptedStateSummary(ctx, nodeID, requestID, summaryIDs)
	return nil
}

func (gh *getter) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	lastAccepted, err := gh.vm.LastAccepted(ctx)
	if err != nil {
		return err
	}
	gh.sender.SendAcceptedFrontier(ctx, nodeID, requestID, lastAccepted)
	return nil
}

func (gh *getter) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	lastAcceptedID, err := gh.vm.LastAccepted(ctx)
	if err != nil {
		return err
	}
	lastAccepted, err := gh.vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return err
	}
	lastAcceptedHeight := lastAccepted.Height()

	acceptedIDs := make([]ids.ID, 0, containerIDs.Len())
	for blkID := range containerIDs {
		blk, err := gh.vm.GetBlock(ctx, blkID)
		if err != nil {
			continue
		}

		height := blk.Height()
		if height > lastAcceptedHeight {
			continue
		}

		acceptedBlkID, err := gh.vm.GetBlockIDAtHeight(ctx, height)
		if err != nil {
			continue
		}

		if blkID == acceptedBlkID {
			acceptedIDs = append(acceptedIDs, blkID)
		}
	}
	gh.sender.SendAccepted(ctx, nodeID, requestID, acceptedIDs)
	return nil
}

func (gh *getter) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) error {
	ancestorsBytes, err := block.GetAncestors(
		ctx,
		gh.log,
		gh.vm,
		blkID,
		gh.maxContainersGetAncestors,
		constants.MaxContainersLen,
		gh.maxTimeGetAncestors,
	)
	if err != nil {
		gh.log.Verbo("dropping GetAncestors message",
			zap.String("reason", "couldn't get ancestors"),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
		return nil
	}

	gh.getAncestorsBlks.Observe(float64(len(ancestorsBytes)))
	gh.sender.SendAncestors(ctx, nodeID, requestID, ancestorsBytes)
	return nil
}

func (gh *getter) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) error {
	blk, err := gh.vm.GetBlock(ctx, blkID)
	if err != nil {
		// If we failed to get the block, that means either an unexpected error
		// has occurred, [vdr] is not following the protocol, or the
		// block has been pruned.
		gh.log.Debug("failed Get request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
		return nil
	}

	// Respond to the validator with the fetched block and the same requestID.
	gh.sender.SendPut(ctx, nodeID, requestID, blk.Bytes())
	return nil
}
