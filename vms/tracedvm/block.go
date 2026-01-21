// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	_ snowman.Block           = (*tracedBlock)(nil)
	_ snowman.OracleBlock     = (*tracedBlock)(nil)
	_ block.WithVerifyContext = (*tracedBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

type tracedBlock struct {
	snowman.Block

	vm *blockVM
}

func (b *tracedBlock) Verify(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, b.vm.verifyTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Verify(ctx)
}

func (b *tracedBlock) Accept(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, b.vm.acceptTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Accept(ctx)
}

func (b *tracedBlock) Reject(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, b.vm.rejectTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Reject(ctx)
}

func (b *tracedBlock) Options(ctx context.Context) ([2]snowman.Block, error) {
	oracleBlock, ok := b.Block.(snowman.OracleBlock)
	if !ok {
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	ctx, span := b.vm.tracer.Start(ctx, b.vm.optionsTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	blks, err := oracleBlock.Options(ctx)
	if err != nil {
		return [2]snowman.Block{}, err
	}
	return [2]snowman.Block{
		&tracedBlock{
			Block: blks[0],
			vm:    b.vm,
		},
		&tracedBlock{
			Block: blks[1],
			vm:    b.vm,
		},
	}, nil
}

func (b *tracedBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := b.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}

	ctx, span := b.vm.tracer.Start(ctx, b.vm.shouldVerifyWithContextTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

func (b *tracedBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	blkWithCtx, ok := b.Block.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, b.Block)
	}

	ctx, span := b.vm.tracer.Start(ctx, b.vm.verifyWithContextTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
		attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
	))
	defer span.End()

	return blkWithCtx.VerifyWithContext(ctx, blockCtx)
}
