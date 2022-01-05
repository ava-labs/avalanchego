// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package msghandler

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

// Get requests are always served. Other messages are dropped

var _ common.Handler = &Handler{}

func New(manager vertex.Manager, commonCfg common.Config) (Handler, error) {
	bh := Handler{
		manager: manager,
		sender:  commonCfg.Sender,
		cfg:     commonCfg,
		log:     commonCfg.Ctx.Log,
	}

	errs := wrappers.Errs{}
	bh.getAncestorsVtxs = metric.NewAveragerWithErrs(
		"bs",
		"get_ancestors_vtxs",
		"vertices fetched in a call to GetAncestors",
		commonCfg.Ctx.Registerer,
		&errs,
	)

	return bh, errs.Err
}

type Handler struct {
	manager vertex.Manager
	sender  common.Sender
	cfg     common.Config

	log              logging.Logger
	getAncestorsVtxs metric.Averager
}

// All Get.* Requests are served ...
func (bh Handler) Get(validatorID ids.ShortID, requestID uint32, vtxID ids.ID) error {
	// If this engine has access to the requested vertex, provide it
	if vtx, err := bh.manager.GetVtx(vtxID); err == nil {
		bh.sender.SendPut(validatorID, requestID, vtxID, vtx.Bytes())
	}
	return nil
}

func (bh Handler) GetAncestors(validatorID ids.ShortID, requestID uint32, vtxID ids.ID) error {
	startTime := time.Now()
	bh.log.Verbo("GetAncestors(%s, %d, %s) called", validatorID, requestID, vtxID)
	vertex, err := bh.manager.GetVtx(vtxID)
	if err != nil || vertex.Status() == choices.Unknown {
		bh.log.Verbo("dropping getAncestors")
		return nil // Don't have the requested vertex. Drop message.
	}

	queue := make([]avalanche.Vertex, 1, bh.cfg.MultiputMaxContainersSent) // for BFS
	queue[0] = vertex
	ancestorsBytesLen := 0                                                // length, in bytes, of vertex and its ancestors
	ancestorsBytes := make([][]byte, 0, bh.cfg.MultiputMaxContainersSent) // vertex and its ancestors in BFS order
	visited := ids.Set{}                                                  // IDs of vertices that have been in queue before
	visited.Add(vertex.ID())

	for len(ancestorsBytes) < bh.cfg.MultiputMaxContainersSent && len(queue) > 0 && time.Since(startTime) < bh.cfg.MaxTimeGetAncestors {
		var vtx avalanche.Vertex
		vtx, queue = queue[0], queue[1:] // pop
		vtxBytes := vtx.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := wrappers.IntLen + ancestorsBytesLen + len(vtxBytes); newLen < constants.MaxContainersLen {
			ancestorsBytes = append(ancestorsBytes, vtxBytes)
			ancestorsBytesLen = newLen
		} else { // reached maximum response size
			break
		}
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

	bh.getAncestorsVtxs.Observe(float64(len(ancestorsBytes)))
	bh.sender.SendMultiPut(validatorID, requestID, ancestorsBytes)
	return nil
}

func (bh Handler) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	// TODO ABENEGIA: Broken common interface with Snowman. To Restore
	// acceptedFrontier, err := b.Bootstrapable.CurrentAcceptedFrontier()

	acceptedFrontier := bh.manager.Edge()
	bh.sender.SendAcceptedFrontier(validatorID, requestID, acceptedFrontier)
	return nil
}

func (bh Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// TODO ABENEGIA: Broken common interface with Snowman. To Restore
	// bh.sender.SendAccepted(validatorID, requestID, b.Bootstrapable.FilterAccepted(containerIDs))

	acceptedVtxIDs := make([]ids.ID, 0, len(containerIDs))
	for _, vtxID := range containerIDs {
		if vtx, err := bh.manager.GetVtx(vtxID); err == nil && vtx.Status() == choices.Accepted {
			acceptedVtxIDs = append(acceptedVtxIDs, vtxID)
		}
	}

	bh.sender.SendAccepted(validatorID, requestID, acceptedVtxIDs)
	return nil
}

// ... all other messages are simply dropped by default
func (bh Handler) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	bh.log.Debug("AcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	bh.log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	bh.log.Debug("Accepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	bh.log.Debug("GetAcceptedFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	bh.log.Debug("AppRequest(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (bh Handler) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	bh.log.Debug("AppRequestFailed(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (bh Handler) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	bh.log.Debug("AppResponse(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (bh Handler) AppGossip(nodeID ids.ShortID, msg []byte) error {
	bh.log.Debug("AppGossip(%s) unhandled by this gear. Dropped.", nodeID)
	return nil
}

func (bh Handler) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		bh.log.Verbo("Gossip Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	} else {
		bh.log.Debug("Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	}
	return nil
}

func (bh Handler) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	bh.log.Debug("MultiPut(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	bh.log.Debug("GetFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	bh.log.Debug("GetAncestorsFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (bh Handler) Gossip() error {
	bh.log.Debug("Gossip unhandled by this gear. Dropped.")
	return nil
}

func (bh Handler) Timeout() error {
	bh.log.Debug("Timeout unhandled by this gear. Dropped.")
	return nil
}

func (bh Handler) Halt() {
	bh.log.Debug("Halt unhandled by this gear. Dropped.")
}

func (bh Handler) Shutdown() error {
	bh.log.Debug("Shutdown unhandled by this gear. Dropped.")
	return nil
}

func (bh Handler) Notify(msg common.Message) error {
	bh.log.Debug("Notify message %s unhandled by this gear. Dropped", msg.String())
	return nil
}

func (bh Handler) Connected(validatorID ids.ShortID, nodeVersion version.Application) error {
	bh.log.Debug("Connected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

func (bh Handler) Disconnected(validatorID ids.ShortID) error {
	bh.log.Debug("Disconnected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

func (bh Handler) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	bh.log.Debug("PullQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (bh Handler) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	bh.log.Debug("PushQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (bh Handler) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	bh.log.Debug("Chits(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

func (bh Handler) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	bh.log.Debug("QueryFailed(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}
