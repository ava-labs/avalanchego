// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) VerifyHeightIndex() error {
	if vm.hVM == nil {
		return block.ErrHeightIndexedVMNotImplemented
	}

	start := vm.clock.Time()
	err := vm.hVM.VerifyHeightIndex()
	end := vm.clock.Time()
	vm.blockMetrics.verifyHeightIndex.Observe(float64(end.Sub(start)))
	return err
}

func (vm *blockVM) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if vm.hVM == nil {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}

	start := vm.clock.Time()
	blockID, err := vm.hVM.GetBlockIDAtHeight(height)
	end := vm.clock.Time()
	vm.blockMetrics.getBlockIDAtHeight.Observe(float64(end.Sub(start)))
	return blockID, err
}
