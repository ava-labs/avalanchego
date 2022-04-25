// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
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

func (vm *VM) GetOngoingSyncStateSummary() (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetOngoingSyncStateSummary()
	if err != nil {
		return nil, err
	}

	var (
		proSummary summary.ProposerSummaryIntf
		proBlk     Block
	)
	if innerSummary.ID() == ids.Empty {
		// summary with emptyID signals no local summary is available in InnerVM
		proSummary, err = summary.BuildEmptyProposerSummary()
	} else {
		proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
		if err != nil {
			// this should never happen, it's proVM being out of sync with innerVM
			vm.ctx.Log.Warn("inner summary unknown to proposer VM. Block height index missing: %s", err)
			return nil, block.ErrUnknownStateSummary
		}
		proBlk, err = vm.getBlock(proBlkID)
		if err != nil {
			return nil, block.ErrUnknownStateSummary
		}
		proSummary, err = summary.BuildProposerSummary(proBlk.Bytes(), innerSummary)
		if err != nil {
			return nil, err
		}
	}

	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		proposerBlock:       proBlk,
		vm:                  vm,
	}, err
}

func (vm *VM) GetLastStateSummary() (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	// Extract inner vm's last state summary
	innerSummary, err := vm.innerStateSyncVM.GetLastStateSummary()
	if err != nil {
		return nil, err // including block.ErrUnknownStateSummary case
	}

	// retrieve ProBlk
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this should never happen, since that would mean proVM has become out of sync with innerVM
		vm.ctx.Log.Warn("inner summary unknown to proposer VM. Block height index missing: %s", err)
		return nil, block.ErrUnknownStateSummary
	}
	proBlk, err := vm.GetBlock(proBlkID)
	if err != nil {
		return nil, block.ErrUnknownStateSummary
	}

	proSummary, err := summary.BuildProposerSummary(proBlk.Bytes(), innerSummary)
	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		vm:                  vm,
	}, err
}

// Note: it's important that ParseStateSummary do not use any index or state
// to allow summaries being parsed also by freshly started node with no previous state.
func (vm *VM) ParseStateSummary(summaryBytes []byte) (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	statelessSummary, err := summary.Parse(summaryBytes)
	if err != nil {
		return nil, err
	}

	innerSummary, err := vm.innerStateSyncVM.ParseStateSummary(statelessSummary.InnerBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse inner summary due to: %w", err)
	}
	block, err := vm.parseBlock(statelessSummary.BlockBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse proposervm block bytes from summary due to: %w", err)
	}

	return &statefulSummary{
		ProposerSummaryIntf: &summary.ProposerSummary{
			StatelessSummaryIntf: statelessSummary,
			SummaryHeight:        innerSummary.Height(),
			SummaryBlock:         block,
		},
		innerSummary:  innerSummary,
		proposerBlock: block,
		vm:            vm,
	}, nil
}

func (vm *VM) GetStateSummary(height uint64) (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetStateSummary(height)
	if err != nil {
		return nil, err // including block.ErrUnknownStateSummary case
	}

	// retrieve ProBlk
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this should never happen, since that would mean proVM has become out of sync with innerVM
		vm.ctx.Log.Warn("Block height index missing at height %d: %s", innerSummary.Height(), err)
		return nil, block.ErrUnknownStateSummary
	}
	proBlk, err := vm.GetBlock(proBlkID)
	if err != nil {
		return nil, block.ErrUnknownStateSummary
	}

	proSummary, err := summary.BuildProposerSummary(proBlk.Bytes(), innerSummary)
	return &statefulSummary{
		ProposerSummaryIntf: proSummary,
		innerSummary:        innerSummary,
		vm:                  vm,
	}, err
}

func (vm *VM) notifyCallback(msg common.Message) error {
	if msg != common.StateSyncDone {
		return nil
	}
	return vm.syncSummary.proposerBlock.acceptOuterBlk()
}
