// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errIsHeightIndexingEnabled = errors.New("unexpectedly called IsEnabled")
	errIsHeightIndexComplete   = errors.New("unexpectedly called IsHeightIndexComplete")
	errGetBlockIDByHeight      = errors.New("unexpectedly called GetBlockIDByHeight")

	_ HeightIndexedChainVM = &TestHeightIndexedVM{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestHeightIndexedVM struct {
	T *testing.T

	CantIsHeightIndexingEnabled bool
	CantIsHeightIndexComplete   bool
	CantGetBlockIDByHeight      bool

	IsHeightIndexingEnabledF func() bool
	IsHeightIndexCompleteF   func() bool
	GetBlockIDByHeightF      func(height uint64) (ids.ID, error)
}

func (vm *TestHeightIndexedVM) IsHeightIndexingEnabled() bool {
	if vm.IsHeightIndexingEnabledF != nil {
		return vm.IsHeightIndexingEnabledF()
	}
	if vm.CantIsHeightIndexingEnabled && vm.T != nil {
		vm.T.Fatal(errIsHeightIndexingEnabled)
	}
	return false
}

func (vm *TestHeightIndexedVM) IsHeightIndexComplete() bool {
	if vm.IsHeightIndexCompleteF != nil {
		return vm.IsHeightIndexCompleteF()
	}
	if vm.CantIsHeightIndexComplete && vm.T != nil {
		vm.T.Fatal(errIsHeightIndexComplete)
	}
	return false
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
