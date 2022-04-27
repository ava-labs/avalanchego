// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"

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
		return nil, err // includes database.ErrNotFound case
	}

	return vm.buildStateSummary(innerSummary)
}

func (vm *VM) GetLastStateSummary() (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	// Extract inner vm's last state summary
	innerSummary, err := vm.innerStateSyncVM.GetLastStateSummary()
	if err != nil {
		return nil, err // including database.ErrNotFound case
	}

	return vm.buildStateSummary(innerSummary)
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

	innerSummary, err := vm.innerStateSyncVM.ParseStateSummary(statelessSummary.InnerSummaryBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse inner summary due to: %w", err)
	}
	block, err := vm.parseBlock(statelessSummary.BlockBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse proposervm block bytes from summary due to: %w", err)
	}

	proposerSummary := summary.NewProposerSummary(statelessSummary, innerSummary.Height())
	return &statefulSummary{
		ProposerSummary: proposerSummary,
		innerSummary:    innerSummary,
		proposerBlock:   block,
		vm:              vm,
	}, nil
}

func (vm *VM) GetStateSummary(height uint64) (block.Summary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetStateSummary(height)
	if err != nil {
		return nil, err // including database.ErrNotFound case
	}

	return vm.buildStateSummary(innerSummary)
}

func (vm *VM) buildStateSummary(innerSummary block.Summary) (*statefulSummary, error) {
	// retrieve ProBlk
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this is an unexpected error that means proVM is out of sync with innerVM
		return nil, err
	}
	proBlk, err := vm.GetBlock(proBlkID)
	if err != nil {
		// this is an unexpected error that means proVM is out of sync with innerVM
		return nil, err
	}

	proSummary, err := summary.BuildProposerSummary(proBlk.Bytes(), innerSummary)
	return &statefulSummary{
		ProposerSummary: proSummary,
		innerSummary:    innerSummary,
		vm:              vm,
	}, err
}
