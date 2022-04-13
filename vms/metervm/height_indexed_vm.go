// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/engine/snowman/block"
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
