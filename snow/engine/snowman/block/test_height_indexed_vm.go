// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errVerifyHeightIndex  = errors.New("unexpectedly called VerifyHeightIndex")
	errGetBlockIDByHeight = errors.New("unexpectedly called GetBlockIDByHeight")

	_ HeightIndexedChainVM = &TestHeightIndexedVM{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestHeightIndexedVM struct {
	T *testing.T

	CantVerifyHeightIndex  bool
	CantGetBlockIDByHeight bool

	VerifyHeightIndexF  func() error
	GetBlockIDByHeightF func(height uint64) (ids.ID, error)
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

func (vm *TestHeightIndexedVM) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	if vm.GetBlockIDByHeightF != nil {
		return vm.GetBlockIDByHeightF(height)
	}
	if vm.CantGetBlockIDByHeight && vm.T != nil {
		vm.T.Fatal(errGetAncestor)
	}
	return ids.Empty, errGetBlockIDByHeight
}
