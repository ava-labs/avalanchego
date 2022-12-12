// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	errGetAncestor       = errors.New("unexpectedly called GetAncestor")
	errBatchedParseBlock = errors.New("unexpectedly called BatchedParseBlock")

	_ BatchedChainVM = (*TestBatchedVM)(nil)
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestBatchedVM struct {
	T *testing.T

	CantGetAncestors    bool
	CantBatchParseBlock bool

	GetAncestorsF func(
		ctx context.Context,
		blkID ids.ID,
		maxBlocksNum int,
		maxBlocksSize int,
		maxBlocksRetrivalTime time.Duration,
	) ([][]byte, error)

	BatchedParseBlockF func(
		ctx context.Context,
		blks [][]byte,
	) ([]snowman.Block, error)
}

func (vm *TestBatchedVM) Default(cant bool) {
	vm.CantGetAncestors = cant
	vm.CantBatchParseBlock = cant
}

func (vm *TestBatchedVM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.GetAncestorsF != nil {
		return vm.GetAncestorsF(
			ctx,
			blkID,
			maxBlocksNum,
			maxBlocksSize,
			maxBlocksRetrivalTime,
		)
	}
	if vm.CantGetAncestors && vm.T != nil {
		vm.T.Fatal(errGetAncestor)
	}
	return nil, errGetAncestor
}

func (vm *TestBatchedVM) BatchedParseBlock(
	ctx context.Context,
	blks [][]byte,
) ([]snowman.Block, error) {
	if vm.BatchedParseBlockF != nil {
		return vm.BatchedParseBlockF(ctx, blks)
	}
	if vm.CantBatchParseBlock && vm.T != nil {
		vm.T.Fatal(errBatchedParseBlock)
	}
	return nil, errBatchedParseBlock
}
