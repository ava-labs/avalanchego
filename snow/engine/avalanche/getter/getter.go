// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Get requests are always served, regardless node state (bootstrapping or normal operations).
var _ common.AllGetsServer = (*getter)(nil)

func New(
	storage vertex.Storage,
	sender common.Sender,
	log logging.Logger,
	maxTimeGetAncestors time.Duration,
	maxContainersGetAncestors int,
	reg prometheus.Registerer,
) (common.AllGetsServer, error) {
	gh := &getter{
		storage:                   storage,
		sender:                    sender,
		log:                       log,
		maxTimeGetAncestors:       maxTimeGetAncestors,
		maxContainersGetAncestors: maxContainersGetAncestors,
	}

	var err error
	gh.getAncestorsVtxs, err = metric.NewAverager(
		"bs",
		"get_ancestors_vtxs",
		"vertices fetched in a call to GetAncestors",
		reg,
	)
	return gh, err
}

type getter struct {
	storage                   vertex.Storage
	sender                    common.Sender
	log                       logging.Logger
	maxTimeGetAncestors       time.Duration
	maxContainersGetAncestors int

	getAncestorsVtxs metric.Averager
}

func (gh *getter) GetStateSummaryFrontier(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	gh.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (gh *getter) GetAcceptedStateSummary(_ context.Context, nodeID ids.NodeID, requestID uint32, _ set.Set[uint64]) error {
	gh.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

// TODO: Remove support for GetAcceptedFrontier messages after v1.11.x is
// activated.
func (gh *getter) GetAcceptedFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32) error {
	acceptedFrontier := gh.storage.Edge(ctx)
	// Since all the DAGs are linearized, we only need to return the stop
	// vertex.
	if len(acceptedFrontier) > 0 {
		gh.sender.SendAcceptedFrontier(ctx, validatorID, requestID, acceptedFrontier[0])
	}
	return nil
}

// TODO: Remove support for GetAccepted messages after v1.11.x is activated.
func (gh *getter) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	acceptedVtxIDs := make([]ids.ID, 0, containerIDs.Len())
	for vtxID := range containerIDs {
		if vtx, err := gh.storage.GetVtx(ctx, vtxID); err == nil && vtx.Status() == choices.Accepted {
			acceptedVtxIDs = append(acceptedVtxIDs, vtxID)
		}
	}
	gh.sender.SendAccepted(ctx, nodeID, requestID, acceptedVtxIDs)
	return nil
}

func (gh *getter) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) error {
	startTime := time.Now()
	gh.log.Verbo("called GetAncestors",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("vtxID", vtxID),
	)
	vertex, err := gh.storage.GetVtx(ctx, vtxID)
	if err != nil || vertex.Status() == choices.Unknown {
		gh.log.Verbo("dropping getAncestors")
		return nil // Don't have the requested vertex. Drop message.
	}

	queue := make([]avalanche.Vertex, 1, gh.maxContainersGetAncestors) // for BFS
	queue[0] = vertex
	ancestorsBytesLen := 0                                            // length, in bytes, of vertex and its ancestors
	ancestorsBytes := make([][]byte, 0, gh.maxContainersGetAncestors) // vertex and its ancestors in BFS order
	visited := set.Of(vertex.ID())                                    // IDs of vertices that have been in queue before

	for len(ancestorsBytes) < gh.maxContainersGetAncestors && len(queue) > 0 && time.Since(startTime) < gh.maxTimeGetAncestors {
		var vtx avalanche.Vertex
		vtx, queue = queue[0], queue[1:] // pop
		vtxBytes := vtx.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		newLen := wrappers.IntLen + ancestorsBytesLen + len(vtxBytes)
		if newLen > constants.MaxContainersLen {
			// reached maximum response size
			break
		}
		ancestorsBytes = append(ancestorsBytes, vtxBytes)
		ancestorsBytesLen = newLen
		parents, err := vtx.Parents()
		if err != nil {
			return err
		}
		for _, parent := range parents {
			if parent.Status() == choices.Unknown { // Don't have this vertex;ignore
				continue
			}
			if parentID := parent.ID(); !visited.Contains(parentID) { // If already visited, ignore
				queue = append(queue, parent)
				visited.Add(parentID)
			}
		}
	}

	gh.getAncestorsVtxs.Observe(float64(len(ancestorsBytes)))
	gh.sender.SendAncestors(ctx, nodeID, requestID, ancestorsBytes)
	return nil
}

func (gh *getter) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) error {
	// If this engine has access to the requested vertex, provide it
	if vtx, err := gh.storage.GetVtx(ctx, vtxID); err == nil {
		gh.sender.SendPut(ctx, nodeID, requestID, vtx.Bytes())
	}
	return nil
}
