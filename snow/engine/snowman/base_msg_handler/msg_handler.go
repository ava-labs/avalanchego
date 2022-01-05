// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package msghandler

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

// Get requests are always served. Other messages are dropped

var _ common.Handler = &Handler{}

func New(vm block.ChainVM, commonCfg common.Config) (Handler, error) {
	bh := Handler{
		vm:     vm,
		sender: commonCfg.Sender,
		cfg:    commonCfg,
		log:    commonCfg.Ctx.Log,
	}

	errs := wrappers.Errs{}
	bh.getAncestorsBlks = metric.NewAveragerWithErrs(
		"bs",
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		commonCfg.Ctx.Registerer,
		&errs,
	)

	return bh, errs.Err
}

type Handler struct {
	vm     block.ChainVM
	sender common.Sender
	cfg    common.Config

	log              logging.Logger
	getAncestorsBlks metric.Averager
}

// All Get.* Requests are served ...
func (bh Handler) Get(validatorID ids.ShortID, requestID uint32, blkID ids.ID) error {
	blk, err := bh.vm.GetBlock(blkID)
	if err != nil {
		// If we failed to get the block, that means either an unexpected error
		// has occurred, [vdr] is not following the protocol, or the
		// block has been pruned.
		bh.log.Debug("Get(%s, %d, %s) failed with: %s", validatorID, requestID, blkID, err)
		return nil
	}

	// Respond to the validator with the fetched block and the same requestID.
	bh.sender.SendPut(validatorID, requestID, blkID, blk.Bytes())
	return nil
}

func (bh Handler) GetAncestors(validatorID ids.ShortID, requestID uint32, blkID ids.ID) error {
	ancestorsBytes, err := block.GetAncestors(
		bh.vm,
		blkID,
		bh.cfg.MultiputMaxContainersSent,
		constants.MaxContainersLen,
		bh.cfg.MaxTimeGetAncestors,
	)
	if err != nil {
		bh.log.Verbo("couldn't get ancestors with %s. Dropping GetAncestors(%s, %d, %s)",
			err, validatorID, requestID, blkID)
		return nil
	}

	bh.getAncestorsBlks.Observe(float64(len(ancestorsBytes)))
	bh.sender.SendMultiPut(validatorID, requestID, ancestorsBytes)
	return nil
}

func (bh Handler) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	// TODO ABENEGIA: Broken common interface with Avalanche. To Restore
	// acceptedFrontier, err := b.Bootstrapable.CurrentAcceptedFrontier()
	acceptedFrontier, err := bh.vm.LastAccepted()
	if err != nil {
		return err
	}
	bh.sender.SendAcceptedFrontier(validatorID, requestID, []ids.ID{acceptedFrontier})
	return nil
}

func (bh Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// TODO ABENEGIA: Broken common interface with Avalanche. To Restore
	// bh.sender.SendAccepted(validatorID, requestID, b.Bootstrapable.FilterAccepted(containerIDs))

	acceptedIDs := make([]ids.ID, 0, len(containerIDs))
	for _, blkID := range containerIDs {
		if blk, err := bh.vm.GetBlock(blkID); err == nil && blk.Status() == choices.Accepted {
			acceptedIDs = append(acceptedIDs, blkID)
		}
	}

	bh.sender.SendAccepted(validatorID, requestID, acceptedIDs)
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
