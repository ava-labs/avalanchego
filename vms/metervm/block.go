// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*meterBlock)(nil)
	_ snowman.OracleBlock     = (*meterBlock)(nil)
	_ block.WithVerifyContext = (*meterBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

type meterBlock struct {
	snowman.Block

	vm *blockVM
}

func (mb *meterBlock) Verify(ctx context.Context) error {
	start := time.Now()
	err := mb.Block.Verify(ctx)
	duration := float64(time.Since(start))
	if err != nil {
		mb.vm.blockMetrics.verifyErr.Observe(duration)
	} else {
		mb.vm.verify.Observe(duration)
	}
	return err
}

func (mb *meterBlock) Accept(ctx context.Context) error {
	start := time.Now()
	err := mb.Block.Accept(ctx)
	duration := float64(time.Since(start))
	mb.vm.blockMetrics.accept.Observe(duration)
	return err
}

func (mb *meterBlock) Reject(ctx context.Context) error {
	start := time.Now()
	err := mb.Block.Reject(ctx)
	duration := float64(time.Since(start))
	mb.vm.blockMetrics.reject.Observe(duration)
	return err
}

func (mb *meterBlock) Options(ctx context.Context) ([2]snowman.Block, error) {
	oracleBlock, ok := mb.Block.(snowman.OracleBlock)
	if !ok {
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	blks, err := oracleBlock.Options(ctx)
	if err != nil {
		return [2]snowman.Block{}, err
	}
	return [2]snowman.Block{
		&meterBlock{
			Block: blks[0],
			vm:    mb.vm,
		},
		&meterBlock{
			Block: blks[1],
			vm:    mb.vm,
		},
	}, nil
}

func (mb *meterBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := mb.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}

	start := time.Now()
	shouldVerify, err := blkWithCtx.ShouldVerifyWithContext(ctx)
	duration := float64(time.Since(start))
	mb.vm.blockMetrics.shouldVerifyWithContext.Observe(duration)
	return shouldVerify, err
}

func (mb *meterBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	blkWithCtx, ok := mb.Block.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, mb.Block)
	}

	start := time.Now()
	err := blkWithCtx.VerifyWithContext(ctx, blockCtx)
	duration := float64(time.Since(start))
	if err != nil {
		mb.vm.blockMetrics.verifyWithContextErr.Observe(duration)
	} else {
		mb.vm.verifyWithContext.Observe(duration)
	}
	return err
}
