// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ block.ChainVM                      = (*blockVM)(nil)
	_ block.BuildBlockWithContextChainVM = (*blockVM)(nil)
	_ block.BatchedChainVM               = (*blockVM)(nil)
	_ block.StateSyncableVM              = (*blockVM)(nil)
)

type blockVM struct {
	block.ChainVM
	buildBlockVM block.BuildBlockWithContextChainVM
	batchedVM    block.BatchedChainVM
	ssVM         block.StateSyncableVM

	blockMetrics
	registry prometheus.Registerer
}

func NewBlockVM(
	vm block.ChainVM,
	reg prometheus.Registerer,
) block.ChainVM {
	buildBlockVM, _ := vm.(block.BuildBlockWithContextChainVM)
	batchedVM, _ := vm.(block.BatchedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:      vm,
		buildBlockVM: buildBlockVM,
		batchedVM:    batchedVM,
		ssVM:         ssVM,
		registry:     reg,
	}
}

func (vm *blockVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	err := vm.blockMetrics.Initialize(
		vm.buildBlockVM != nil,
		vm.batchedVM != nil,
		vm.ssVM != nil,
		vm.registry,
	)
	if err != nil {
		return err
	}

	return vm.ChainVM.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, fxs, appSender)
}

func (vm *blockVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	start := time.Now()
	blk, err := vm.ChainVM.BuildBlock(ctx)
	duration := float64(time.Since(start))
	if err != nil {
		vm.blockMetrics.buildBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.buildBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	start := time.Now()
	blk, err := vm.ChainVM.ParseBlock(ctx, b)
	duration := float64(time.Since(start))
	if err != nil {
		vm.blockMetrics.parseBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.parseBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	start := time.Now()
	blk, err := vm.ChainVM.GetBlock(ctx, id)
	duration := float64(time.Since(start))
	if err != nil {
		vm.blockMetrics.getBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.getBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) SetPreference(ctx context.Context, id ids.ID) error {
	start := time.Now()
	err := vm.ChainVM.SetPreference(ctx, id)
	vm.blockMetrics.setPreference.Observe(float64(time.Since(start)))
	return err
}

func (vm *blockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	start := time.Now()
	lastAcceptedID, err := vm.ChainVM.LastAccepted(ctx)
	vm.blockMetrics.lastAccepted.Observe(float64(time.Since(start)))
	return lastAcceptedID, err
}

func (vm *blockVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	start := time.Now()
	blockID, err := vm.ChainVM.GetBlockIDAtHeight(ctx, height)
	vm.blockMetrics.getBlockIDAtHeight.Observe(float64(time.Since(start)))
	return blockID, err
}
