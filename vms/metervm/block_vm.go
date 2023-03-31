// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	_ block.ChainVM                      = (*blockVM)(nil)
	_ block.BuildBlockWithContextChainVM = (*blockVM)(nil)
	_ block.BatchedChainVM               = (*blockVM)(nil)
	_ block.HeightIndexedChainVM         = (*blockVM)(nil)
	_ block.StateSyncableVM              = (*blockVM)(nil)
)

type blockVM struct {
	block.ChainVM
	buildBlockVM block.BuildBlockWithContextChainVM
	batchedVM    block.BatchedChainVM
	hVM          block.HeightIndexedChainVM
	ssVM         block.StateSyncableVM

	blockMetrics
	clock mockable.Clock
}

func NewBlockVM(vm block.ChainVM) block.ChainVM {
	buildBlockVM, _ := vm.(block.BuildBlockWithContextChainVM)
	batchedVM, _ := vm.(block.BatchedChainVM)
	hVM, _ := vm.(block.HeightIndexedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:      vm,
		buildBlockVM: buildBlockVM,
		batchedVM:    batchedVM,
		hVM:          hVM,
		ssVM:         ssVM,
	}
}

func (vm *blockVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	registerer := prometheus.NewRegistry()
	err := vm.blockMetrics.Initialize(
		vm.buildBlockVM != nil,
		vm.batchedVM != nil,
		vm.hVM != nil,
		vm.ssVM != nil,
		"",
		registerer,
	)
	if err != nil {
		return err
	}

	optionalGatherer := metrics.NewOptionalGatherer()
	multiGatherer := metrics.NewMultiGatherer()
	if err := multiGatherer.Register("metervm", registerer); err != nil {
		return err
	}
	if err := multiGatherer.Register("", optionalGatherer); err != nil {
		return err
	}
	if err := chainCtx.Metrics.Register(multiGatherer); err != nil {
		return err
	}
	chainCtx.Metrics = optionalGatherer

	return vm.ChainVM.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs, appSender)
}

func (vm *blockVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.BuildBlock(ctx)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
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
	start := vm.clock.Time()
	blk, err := vm.ChainVM.ParseBlock(ctx, b)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
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
	start := vm.clock.Time()
	blk, err := vm.ChainVM.GetBlock(ctx, id)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
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
	start := vm.clock.Time()
	err := vm.ChainVM.SetPreference(ctx, id)
	end := vm.clock.Time()
	vm.blockMetrics.setPreference.Observe(float64(end.Sub(start)))
	return err
}

func (vm *blockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	start := vm.clock.Time()
	lastAcceptedID, err := vm.ChainVM.LastAccepted(ctx)
	end := vm.clock.Time()
	vm.blockMetrics.lastAccepted.Observe(float64(end.Sub(start)))
	return lastAcceptedID, err
}
