// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	_ snowman.Block       = (*tracedBlock)(nil)
	_ snowman.OracleBlock = (*tracedBlock)(nil)
)

type tracedBlock struct {
	snowman.Block

	vm *blockVM
}

func (b *tracedBlock) Verify(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, "tracedBlock.Verify", oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Verify(ctx)
}

func (b *tracedBlock) Accept(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, "tracedBlock.Accept", oteltrace.WithAttributes(
		attribute.Stringer("blkID", b.ID()),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Accept(ctx)
}

func (b *tracedBlock) Reject(ctx context.Context) error {
	ctx, span := b.vm.tracer.Start(ctx, "tracedBlock.Reject", oteltrace.WithAttributes(
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

	ctx, span := b.vm.tracer.Start(ctx, "tracedBlock.Options", oteltrace.WithAttributes(
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
