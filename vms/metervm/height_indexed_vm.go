// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ block.HeightIndexedChainVM = &blockVM{}

func (vm *blockVM) VerifyHeightIndex() error {
	hVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return block.ErrHeightIndexedVMNotImplemented
	}
	return hVM.VerifyHeightIndex()
}

func (vm *blockVM) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	hVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}
	return hVM.GetBlockIDAtHeight(height)
}
