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
		return nil, err // including common.ErrNoStateSyncOngoing case
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

func (vm *VM) SetSyncableStateSummaries(accepted []common.Summary) error {
	if vm.innerStateSyncVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	coreSummaries := make([]common.Summary, 0, len(accepted))
	for _, s := range accepted {
		proContent, err := summary.Parse(s.Bytes())
		if err != nil {
			return err
		}

		coreSummary, err := vm.innerStateSyncVM.ParseStateSummary(proContent.InnerBytes())
		if err != nil {
			return fmt.Errorf("could not parse coreSummaryContent due to: %w", err)
		}

		coreSummaries = append(coreSummaries, coreSummary)

		// Following state sync introduction, we update height -> blockID index
		// with summaries content in order to support resuming state sync in case
		// of shutdown. The height index allows to retrieve the proposerBlkID
		// of any state sync passed down to coreVM, so that the proposerVM state summary
		// information of any coreVM summary can be rebuilt and pass to the engine, even
		// following a shutdown.
		// Note that we won't download all the blocks associated with state summaries,
		// so proposerVM may not not all the full blocks indexed into height index. Same
		// is true for coreVM.
		if err := vm.updateHeightIndex(s.Height(), proContent.ProposerBlockID()); err != nil {
			return err
		}
	}

	if err := vm.db.Commit(); err != nil {
		return nil
	}

	return vm.innerStateSyncVM.SetSyncableStateSummaries(coreSummaries)
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
