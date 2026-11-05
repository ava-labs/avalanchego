// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package hookadaptor converts hook points that don't depend on generic types
// into [hook.PointsG].
package hookadaptor

import (
	"context"
	"iter"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// Points is a subset of [hook.PointsG] that doesn't expose any end-of-block
// operations.
type Points interface {
	BlockBuilder
	BlockRebuilderFrom(block *types.Block) (BlockBuilder, error)

	ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error)
	GasConfigAfter(*types.Header) (target gas.Gas, c gastime.GasPriceConfig)
	BlockTime(h *types.Header) time.Time
	SettledBy(*types.Header) hook.Settled
	VerifyBlockSyntax(*types.Block) error
	CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error
	StartExecutingBlock(rules params.Rules, statedb *state.StateDB, parent *types.Header, block *types.Block) error
	FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error
	AfterExecutingBlock(*types.Block, types.Receipts) error
}

// BlockBuilder is a subset of [hook.BlockBuilder] that doesn't expose any
// end-of-block operations.
type BlockBuilder interface {
	BuildHeader(parent *types.Header) (*types.Header, error)
	BuildBlock(
		header *types.Header,
		blockCtx *block.Context,
		txs []*types.Transaction,
		receipts []*types.Receipt,
		settled hook.Settled,
	) (*types.Block, error)
}

// noTx is a placeholder [hook.Transaction] type. It is never instantiated, as
// the adaptors never produce end-of-block operations.
type noTx struct{}

func (noTx) AsOp() hook.Op { return hook.Op{} }
func (noTx) Size() uint64  { return 0 }

var (
	_ hook.PointsG[noTx]      = points{}
	_ hook.BlockBuilder[noTx] = blockBuilder{}
)

// Convert converts [Points] into a [hook.PointsG]. The returned value never
// reports any end-of-block operations.
func Convert(p Points) hook.PointsG[noTx] {
	return points{p}
}

type points struct {
	Points
}

func (points) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

func emptySeq[T any](func(T) bool) {}

func (points) PotentialEndOfBlockOps(context.Context, *types.Header, common.Hash, saetypes.BlockSource) iter.Seq[noTx] {
	return emptySeq[noTx]
}

func (p points) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []noTx,
	settled hook.Settled,
) (*types.Block, error) {
	return p.Points.BuildBlock(header, blockCtx, txs, receipts, settled)
}

func (p points) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[noTx], error) {
	builder, err := p.Points.BlockRebuilderFrom(b)
	if err != nil {
		return nil, err
	}
	return blockBuilder{builder}, nil
}

type blockBuilder struct {
	BlockBuilder
}

func (blockBuilder) PotentialEndOfBlockOps(context.Context, *types.Header, common.Hash, saetypes.BlockSource) iter.Seq[noTx] {
	return emptySeq[noTx]
}

func (b blockBuilder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []noTx,
	settled hook.Settled,
) (*types.Block, error) {
	return b.BlockBuilder.BuildBlock(header, blockCtx, txs, receipts, settled)
}
