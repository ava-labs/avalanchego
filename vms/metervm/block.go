// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	_ snowman.Block       = (*meterBlock)(nil)
	_ snowman.OracleBlock = (*meterBlock)(nil)
)

type meterBlock struct {
	snowman.Block

	vm *blockVM
}

func (mb *meterBlock) Verify(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Verify(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		mb.vm.blockMetrics.verifyErr.Observe(duration)
	} else {
		mb.vm.verify.Observe(duration)
	}
	return err
}

func (mb *meterBlock) Accept(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Accept(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.accept.Observe(duration)
	return err
}

func (mb *meterBlock) Reject(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Reject(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
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
