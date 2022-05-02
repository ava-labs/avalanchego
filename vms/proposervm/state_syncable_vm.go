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

func (vm *VM) GetOngoingSyncStateSummary() (block.StateSummary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetOngoingSyncStateSummary()
	if err != nil {
		return nil, err // includes database.ErrNotFound case
	}

	return vm.buildStateSummary(innerSummary)
}

func (vm *VM) GetLastStateSummary() (block.StateSummary, error) {
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
func (vm *VM) ParseStateSummary(summaryBytes []byte) (block.StateSummary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	statelessSummary, err := summary.Parse(summaryBytes)
	if err != nil {
		// it may be a preFork summary
		return vm.innerStateSyncVM.ParseStateSummary(summaryBytes)
	}

	innerSummary, err := vm.innerStateSyncVM.ParseStateSummary(statelessSummary.InnerSummaryBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse inner summary due to: %w", err)
	}
	block, err := vm.parseBlock(statelessSummary.BlockBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse proposervm block bytes from summary due to: %w", err)
	}

	return &stateSummary{
		StateSummary: statelessSummary,
		innerSummary: innerSummary,
		block:        block,
	}, nil
}

func (vm *VM) GetStateSummary(height uint64) (block.StateSummary, error) {
	if vm.innerStateSyncVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.innerStateSyncVM.GetStateSummary(height)
	if err != nil {
		return nil, err // including database.ErrNotFound case
	}

	return vm.buildStateSummary(innerSummary)
}

func (vm *VM) buildStateSummary(innerSummary block.StateSummary) (block.StateSummary, error) {
	forkHeight, err := vm.GetForkHeight()
	if err != nil {
		return nil, err
	}

	if innerSummary.Height() < forkHeight {
		return innerSummary, nil
	}

	// retrieve ProBlk
	proBlkID, err := vm.GetBlockIDAtHeight(innerSummary.Height())
	if err != nil {
		// this is an unexpected error that means proVM is out of sync with innerVM
		return nil, err
	}
	proBlk, err := vm.getBlock(proBlkID)
	if err != nil {
		// this is an unexpected error that means proVM is out of sync with innerVM
		return nil, err
	}

	statelessSummary, err := summary.Build(proBlk.Bytes(), innerSummary.Bytes())
	return &stateSummary{
		StateSummary: statelessSummary,
		innerSummary: innerSummary,
		block:        proBlk,
	}, err
}
