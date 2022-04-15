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

package block

import (
	"errors"
	"testing"

	"github.com/chain4travel/caminogo/ids"
)

var (
	errVerifyHeightIndex  = errors.New("unexpectedly called VerifyHeightIndex")
	errGetBlockIDAtHeight = errors.New("unexpectedly called GetBlockIDAtHeight")

	_ HeightIndexedChainVM = &TestHeightIndexedVM{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestHeightIndexedVM struct {
	T *testing.T

	CantVerifyHeightIndex  bool
	CantGetBlockIDAtHeight bool

	VerifyHeightIndexF  func() error
	GetBlockIDAtHeightF func(height uint64) (ids.ID, error)
}

func (vm *TestHeightIndexedVM) VerifyHeightIndex() error {
	if vm.VerifyHeightIndexF != nil {
		return vm.VerifyHeightIndexF()
	}
	if vm.CantVerifyHeightIndex && vm.T != nil {
		vm.T.Fatal(errVerifyHeightIndex)
	}
	return errVerifyHeightIndex
}

func (vm *TestHeightIndexedVM) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(height)
	}
	if vm.CantGetBlockIDAtHeight && vm.T != nil {
		vm.T.Fatal(errGetAncestor)
	}
	return ids.Empty, errGetBlockIDAtHeight
}
