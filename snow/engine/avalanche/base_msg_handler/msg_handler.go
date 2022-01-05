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
)

// Get requests are always served. Hence this object, common to bootstrapper and engine
var (
	_ common.GetAcceptedFrontierHandler = &Handler{}
	_ common.GetAcceptedHandler         = &Handler{}
	_ common.GetAncestorsHandler        = &Handler{}
	_ common.GetHandler                 = &Handler{}
)

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
