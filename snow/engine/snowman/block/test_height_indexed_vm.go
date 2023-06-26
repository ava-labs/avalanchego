// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errVerifyHeightIndex  = errors.New("unexpectedly called VerifyHeightIndex")
	errGetBlockIDAtHeight = errors.New("unexpectedly called GetBlockIDAtHeight")

	_ HeightIndexedChainVM = (*TestHeightIndexedVM)(nil)
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestHeightIndexedVM struct {
	T *testing.T

	CantVerifyHeightIndex  bool
	CantGetBlockIDAtHeight bool

	VerifyHeightIndexF  func(context.Context) error
	GetBlockIDAtHeightF func(ctx context.Context, height uint64) (ids.ID, error)
}

func (vm *TestHeightIndexedVM) VerifyHeightIndex(ctx context.Context) error {
	if vm.VerifyHeightIndexF != nil {
		return vm.VerifyHeightIndexF(ctx)
	}
	if vm.CantVerifyHeightIndex && vm.T != nil {
		vm.T.Fatal(errVerifyHeightIndex)
	}
	return errVerifyHeightIndex
}

func (vm *TestHeightIndexedVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(ctx, height)
	}
	if vm.CantGetBlockIDAtHeight && vm.T != nil {
		vm.T.Fatal(errGetAncestor)
	}
	return ids.Empty, errGetBlockIDAtHeight
}
