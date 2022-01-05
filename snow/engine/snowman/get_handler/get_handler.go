// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gethandler

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Get requests are always served. Hence Handler, common to bootstrapper and engine
var _ common.AllGetsServer = &Handler{}

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

func (bh Handler) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	acceptedFrontier, err := bh.CurrentAcceptedFrontier()
	if err != nil {
		return err
	}
	bh.sender.SendAcceptedFrontier(validatorID, requestID, acceptedFrontier)
	return nil
}

func (bh Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	bh.sender.SendAccepted(validatorID, requestID, bh.FilterAccepted(containerIDs))
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

// CurrentAcceptedFrontier returns the set of containerIDs that are accepted,
// but have no accepted children.
// CurrentAcceptedFrontier returns the last accepted block
func (bh Handler) CurrentAcceptedFrontier() ([]ids.ID, error) {
	lastAccepted, err := bh.vm.LastAccepted()
	return []ids.ID{lastAccepted}, err
}

// FilterAccepted returns the subset of containerIDs that are accepted by this chain.
// FilterAccepted returns the blocks in [containerIDs] that we have accepted
func (bh Handler) FilterAccepted(containerIDs []ids.ID) []ids.ID {
	acceptedIDs := make([]ids.ID, 0, len(containerIDs))
	for _, blkID := range containerIDs {
		if blk, err := bh.vm.GetBlock(blkID); err == nil && blk.Status() == choices.Accepted {
			acceptedIDs = append(acceptedIDs, blkID)
		}
	}
	return acceptedIDs
}
