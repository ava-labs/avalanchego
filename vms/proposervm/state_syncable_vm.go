// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

var (
	errUnknownLastSummaryBlockID = errors.New("could not retrieve blockID associated with last summary")
	errBadLastSummaryBlock       = errors.New("could not parse last summary block")
)

func (vm *VM) StateSyncEnabled() (bool, error) {
	if vm.innerStateSyncVM == nil {
		return false, nil
	}

	// if vm implements Snowman++, a block height index must be available
	// to support state sync
	if vm.VerifyHeightIndex() != nil {
		return false, nil
	}

	return vm.innerStateSyncVM.StateSyncEnabled()
}

func (vm *VM) GetOngoingStateSyncSummary() (common.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetOngoingStateSyncSummary()
	if err != nil {
		return nil, err
	}

	var proSummary summary.ProposerSummaryIntf
	if innerSummary.ID() == ids.Empty {
		// summary with emptyID signals no local summary is available in InnerVM
		proSummary, err = summary.BuildEmptyProposerSummary(innerSummary)
	} else {
		var proBlkID ids.ID
		proBlkID, err = vm.GetBlockIDAtHeight(innerSummary.Height())
		if err != nil {
			// this should never happen, it's proVM being out of sync with coreVM
			vm.ctx.Log.Warn("core summary unknown to proposer VM. Block height index missing: %s", err)
			return nil, common.ErrUnknownStateSummary
		}
		proSummary, err = summary.BuildProposerSummary(proBlkID, innerSummary)
	}

	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		vm:                  vm,
	}, err
}

func (vm *VM) GetLastStateSummary() (common.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	// Extract core last state summary
	innerSummary, err := vm.innerStateSyncVM.GetLastStateSummary()
	if err != nil {
		return nil, err // including common.ErrUnknownStateSummary case
	}

	// retrieve ProBlkID
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this should never happen, it's proVM being out of sync with coreVM
		vm.ctx.Log.Warn("core summary unknown to proposer VM. Block height index missing: %s", err)
		return nil, common.ErrUnknownStateSummary
	}

	proSummary, err := summary.BuildProposerSummary(proBlkID, innerSummary)
	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		vm:                  vm,
	}, err
}

// Note: it's important that ParseStateSummary do not use any index or state
// to allow summaries being parsed also by freshly started node with no previous state.
func (vm *VM) ParseStateSummary(summaryBytes []byte) (common.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	statelessSummary, err := summary.Parse(summaryBytes)
	if err != nil {
		return nil, err
	}

	innerSummary, err := vm.innerStateSyncVM.ParseStateSummary(statelessSummary.InnerBytes())
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal coreSummaryContent due to: %w", err)
	}

	return &statefulSummary{
		ProposerSummaryIntf: &summary.ProposerSummary{
			StatelessSummary: *statelessSummary.(*summary.StatelessSummary),
			SummaryHeight:    innerSummary.Height(),
		},
		innerSummary: innerSummary,
		vm:           vm,
	}, nil
}

func (vm *VM) GetStateSummary(height uint64) (common.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetStateSummary(height)
	if err != nil {
		return nil, err // including common.ErrUnknownStateSummary case
	}

	// retrieve ProBlkID
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this should never happen, it's proVM being out of sync with coreVM
		vm.ctx.Log.Warn("core summary unknown to proposer VM. Block height index missing: %s", err)
		return nil, common.ErrUnknownStateSummary
	}

	proSummary, err := summary.BuildProposerSummary(proBlkID, innerSummary)
	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		vm:                  vm,
	}, err
}

func (vm *VM) GetStateSyncResult() (ids.ID, uint64, error) {
	if vm.innerStateSyncVM == nil {
		return ids.Empty, 0, common.ErrStateSyncableVMNotImplemented
	}

	_, height, err := vm.innerStateSyncVM.GetStateSyncResult()
	if err != nil {
		return ids.Empty, 0, err
	}
	proBlkID, err := vm.GetBlockIDAtHeight(height)
	if err != nil {
		return ids.Empty, 0, errUnknownLastSummaryBlockID
	}
	return proBlkID, height, nil
}

func (vm *VM) SetLastStateSummaryBlock(blkBytes []byte) error {
	if vm.innerStateSyncVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	// retrieve core block
	var (
		coreBlkBytes []byte
		blk          Block
		err          error
	)
	if blk, err = vm.parsePostForkBlock(blkBytes); err == nil {
		coreBlkBytes = blk.getInnerBlk().Bytes()
	} else if blk, err = vm.parsePreForkBlock(blkBytes); err == nil {
		coreBlkBytes = blk.Bytes()
	} else {
		return errBadLastSummaryBlock
	}

	if err := blk.acceptOuterBlk(); err != nil {
		return err
	}

	return vm.innerStateSyncVM.SetLastStateSummaryBlock(coreBlkBytes)
}
