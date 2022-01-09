// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
)

// Get requests are always served, regardless node state (bootstrapping or normal operations).
var _ common.AllGetsServer = &getter{}

func New(vm block.ChainVM, commonCfg common.Config) (common.AllGetsServer, error) {
	gh := &getter{
		vm:     vm,
		sender: commonCfg.Sender,
		cfg:    commonCfg,
		log:    commonCfg.Ctx.Log,
	}

	var err error
	gh.getAncestorsBlks, err = metric.NewAverager(
		"bs",
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		commonCfg.Ctx.Registerer,
	)
	return gh, err
}

type getter struct {
	vm     block.ChainVM
	sender common.Sender
	cfg    common.Config

	log              logging.Logger
	getAncestorsBlks metric.Averager
}

func (gh *getter) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	lastAccepted, err := gh.vm.LastAccepted()
	if err != nil {
		return err
	}
	gh.sender.SendAcceptedFrontier(validatorID, requestID, []ids.ID{lastAccepted})
	return nil
}

func (gh *getter) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	acceptedIDs := make([]ids.ID, 0, len(containerIDs))
	for _, blkID := range containerIDs {
		if blk, err := gh.vm.GetBlock(blkID); err == nil && blk.Status() == choices.Accepted {
			acceptedIDs = append(acceptedIDs, blkID)
		}
	}
	gh.sender.SendAccepted(validatorID, requestID, acceptedIDs)
	return nil
}

func (gh *getter) GetAncestors(validatorID ids.ShortID, requestID uint32, blkID ids.ID) error {
	ancestorsBytes, err := block.GetAncestors(
		gh.vm,
		blkID,
		gh.cfg.AncestorsMaxContainersSent,
		constants.MaxContainersLen,
		gh.cfg.MaxTimeGetAncestors,
	)
	if err != nil {
		gh.log.Verbo("couldn't get ancestors with %s. Dropping GetAncestors(%s, %d, %s)",
			err, validatorID, requestID, blkID)
		return nil
	}

	gh.getAncestorsBlks.Observe(float64(len(ancestorsBytes)))
	gh.sender.SendAncestors(validatorID, requestID, ancestorsBytes)
	return nil
}

func (gh *getter) Get(validatorID ids.ShortID, requestID uint32, blkID ids.ID) error {
	blk, err := gh.vm.GetBlock(blkID)
	if err != nil {
		// If we failed to get the block, that means either an unexpected error
		// has occurred, [vdr] is not following the protocol, or the
		// block has been pruned.
		gh.log.Debug("Get(%s, %d, %s) failed with: %s", validatorID, requestID, blkID, err)
		return nil
	}

	// Respond to the validator with the fetched block and the same requestID.
	gh.sender.SendPut(validatorID, requestID, blkID, blk.Bytes())
	return nil
}
