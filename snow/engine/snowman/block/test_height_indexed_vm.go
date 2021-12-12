// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errGetBlockIDByHeight    = errors.New("unexpectedly called GetBlockIDByHeight")
	errHeightIndexingEnabled = errors.New("unexpectedly called HeightIndexingEnabled")

	_ HeightIndexedChainVM = &TestHeightIndexedVM{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestHeightIndexedVM struct {
	T *testing.T

	CantGetBlockIDByHeight bool
	CantIsEnabled          bool

	GetBlockIDByHeightF    func(height uint64) (ids.ID, error)
	HeightIndexingEnabledF func() bool
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

func (vm *TestHeightIndexedVM) HeightIndexingEnabled() bool {
	if vm.HeightIndexingEnabledF != nil {
		return vm.HeightIndexingEnabled()
	}
	if vm.CantIsEnabled && vm.T != nil {
		vm.T.Fatal(errHeightIndexingEnabled)
	}
	return false
}
