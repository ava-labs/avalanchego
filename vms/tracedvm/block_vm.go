// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/trace"
)

var (
	_ block.ChainVM              = (*blockVM)(nil)
	_ block.BatchedChainVM       = (*blockVM)(nil)
	_ block.HeightIndexedChainVM = (*blockVM)(nil)
	_ block.StateSyncableVM      = (*blockVM)(nil)
)

type blockVM struct {
	block.ChainVM
	bVM           block.BatchedChainVM
	hVM           block.HeightIndexedChainVM
	ssVM          block.StateSyncableVM
	initialize    string
	buildBlock    string
	parseBlock    string
	getBlock      string
	setPreference string
	lastAccepted  string
	verify        string
	accept        string
	reject        string
	options       string
	tracer        trace.Tracer
}

func NewBlockVM(vm block.ChainVM, name string, tracer trace.Tracer) block.ChainVM {
	bVM, _ := vm.(block.BatchedChainVM)
	hVM, _ := vm.(block.HeightIndexedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:       vm,
		bVM:           bVM,
		hVM:           hVM,
		ssVM:          ssVM,
		initialize:    fmt.Sprintf("%s.initialize", name),
		buildBlock:    fmt.Sprintf("%s.buildBlock", name),
		parseBlock:    fmt.Sprintf("%s.parseBlock", name),
		getBlock:      fmt.Sprintf("%s.getBlock", name),
		setPreference: fmt.Sprintf("%s.setPreference", name),
		lastAccepted:  fmt.Sprintf("%s.lastAccepted", name),
		verify:        fmt.Sprintf("%s.verify", name),
		accept:        fmt.Sprintf("%s.accept", name),
		reject:        fmt.Sprintf("%s.reject", name),
		options:       fmt.Sprintf("%s.options", name),
		tracer:        tracer,
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
	ctx, span := vm.tracer.Start(ctx, vm.initialize)
	defer span.End()

	return vm.ChainVM.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs, appSender)
}

func (vm *blockVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	ctx, span := vm.tracer.Start(ctx, vm.buildBlock)
	defer span.End()

	blk, err := vm.ChainVM.BuildBlock(ctx)
	return &tracedBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *blockVM) ParseBlock(ctx context.Context, block []byte) (snowman.Block, error) {
	ctx, span := vm.tracer.Start(ctx, vm.parseBlock, oteltrace.WithAttributes(
		attribute.Int("blockLen", len(block)),
	))
	defer span.End()

	blk, err := vm.ChainVM.ParseBlock(ctx, block)
	return &tracedBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *blockVM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	ctx, span := vm.tracer.Start(ctx, vm.getBlock, oteltrace.WithAttributes(
		attribute.Stringer("blkID", blkID),
	))
	defer span.End()

	blk, err := vm.ChainVM.GetBlock(ctx, blkID)
	return &tracedBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *blockVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	ctx, span := vm.tracer.Start(ctx, vm.setPreference, oteltrace.WithAttributes(
		attribute.Stringer("blkID", blkID),
	))
	defer span.End()

	return vm.ChainVM.SetPreference(ctx, blkID)
}

func (vm *blockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	ctx, span := vm.tracer.Start(ctx, vm.lastAccepted)
	defer span.End()

	return vm.ChainVM.LastAccepted(ctx)
}
