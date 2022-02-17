// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	return vm.hVM.VerifyHeightIndex()
}

func (vm *blockVM) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if vm.hVM == nil {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}
	return vm.hVM.GetBlockIDAtHeight(height)
}
