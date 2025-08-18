// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/trace"

	oteltrace "go.opentelemetry.io/otel/trace"
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
	// ChainVM tags
	initializeTag              string
	buildBlockTag              string
	parseBlockTag              string
	getBlockTag                string
	setPreferenceTag           string
	lastAcceptedTag            string
	verifyTag                  string
	acceptTag                  string
	rejectTag                  string
	optionsTag                 string
	shouldVerifyWithContextTag string
	verifyWithContextTag       string
	// BuildBlockWithContextChainVM tags
	buildBlockWithContextTag string
	// BatchedChainVM tags
	getAncestorsTag      string
	batchedParseBlockTag string
	// HeightIndexedChainVM tags
	getBlockIDAtHeightTag string
	// StateSyncableVM tags
	stateSyncEnabledTag           string
	getOngoingSyncStateSummaryTag string
	getLastStateSummaryTag        string
	parseStateSummaryTag          string
	getStateSummaryTag            string
	tracer                        trace.Tracer
}

func NewBlockVM(vm block.ChainVM, name string, tracer trace.Tracer) block.ChainVM {
	buildBlockVM, _ := vm.(block.BuildBlockWithContextChainVM)
	batchedVM, _ := vm.(block.BatchedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:                       vm,
		buildBlockVM:                  buildBlockVM,
		batchedVM:                     batchedVM,
		ssVM:                          ssVM,
		initializeTag:                 name + ".initialize",
		buildBlockTag:                 name + ".buildBlock",
		parseBlockTag:                 name + ".parseBlock",
		getBlockTag:                   name + ".getBlock",
		setPreferenceTag:              name + ".setPreference",
		lastAcceptedTag:               name + ".lastAccepted",
		verifyTag:                     name + ".verify",
		acceptTag:                     name + ".accept",
		rejectTag:                     name + ".reject",
		optionsTag:                    name + ".options",
		shouldVerifyWithContextTag:    name + ".shouldVerifyWithContext",
		verifyWithContextTag:          name + ".verifyWithContext",
		buildBlockWithContextTag:      name + ".buildBlockWithContext",
		getAncestorsTag:               name + ".getAncestors",
		batchedParseBlockTag:          name + ".batchedParseBlock",
		getBlockIDAtHeightTag:         name + ".getBlockIDAtHeight",
		stateSyncEnabledTag:           name + ".stateSyncEnabled",
		getOngoingSyncStateSummaryTag: name + ".getOngoingSyncStateSummary",
		getLastStateSummaryTag:        name + ".getLastStateSummary",
		parseStateSummaryTag:          name + ".parseStateSummary",
		getStateSummaryTag:            name + ".getStateSummary",
		tracer:                        tracer,
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
	ctx, span := vm.tracer.Start(ctx, vm.initializeTag)
	defer span.End()

	return vm.ChainVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		fxs,
		appSender,
	)
}

func (vm *blockVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	ctx, span := vm.tracer.Start(ctx, vm.buildBlockTag)
	defer span.End()

	blk, err := vm.ChainVM.BuildBlock(ctx)
	return &tracedBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *blockVM) ParseBlock(ctx context.Context, block []byte) (snowman.Block, error) {
	ctx, span := vm.tracer.Start(ctx, vm.parseBlockTag, oteltrace.WithAttributes(
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
	ctx, span := vm.tracer.Start(ctx, vm.getBlockTag, oteltrace.WithAttributes(
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
	ctx, span := vm.tracer.Start(ctx, vm.setPreferenceTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", blkID),
	))
	defer span.End()

	return vm.ChainVM.SetPreference(ctx, blkID)
}

func (vm *blockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	ctx, span := vm.tracer.Start(ctx, vm.lastAcceptedTag)
	defer span.End()

	return vm.ChainVM.LastAccepted(ctx)
}

func (vm *blockVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	ctx, span := vm.tracer.Start(ctx, vm.getBlockIDAtHeightTag, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
	))
	defer span.End()

	return vm.ChainVM.GetBlockIDAtHeight(ctx, height)
}
