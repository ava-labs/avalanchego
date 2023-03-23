// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	verifyHeightIndexTag  string
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
	hVM, _ := vm.(block.HeightIndexedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:                       vm,
		buildBlockVM:                  buildBlockVM,
		batchedVM:                     batchedVM,
		hVM:                           hVM,
		ssVM:                          ssVM,
		initializeTag:                 fmt.Sprintf("%s.initialize", name),
		buildBlockTag:                 fmt.Sprintf("%s.buildBlock", name),
		parseBlockTag:                 fmt.Sprintf("%s.parseBlock", name),
		getBlockTag:                   fmt.Sprintf("%s.getBlock", name),
		setPreferenceTag:              fmt.Sprintf("%s.setPreference", name),
		lastAcceptedTag:               fmt.Sprintf("%s.lastAccepted", name),
		verifyTag:                     fmt.Sprintf("%s.verify", name),
		acceptTag:                     fmt.Sprintf("%s.accept", name),
		rejectTag:                     fmt.Sprintf("%s.reject", name),
		optionsTag:                    fmt.Sprintf("%s.options", name),
		shouldVerifyWithContextTag:    fmt.Sprintf("%s.shouldVerifyWithContext", name),
		verifyWithContextTag:          fmt.Sprintf("%s.verifyWithContext", name),
		buildBlockWithContextTag:      fmt.Sprintf("%s.buildBlockWithContext", name),
		getAncestorsTag:               fmt.Sprintf("%s.getAncestors", name),
		batchedParseBlockTag:          fmt.Sprintf("%s.batchedParseBlock", name),
		verifyHeightIndexTag:          fmt.Sprintf("%s.verifyHeightIndex", name),
		getBlockIDAtHeightTag:         fmt.Sprintf("%s.getBlockIDAtHeight", name),
		stateSyncEnabledTag:           fmt.Sprintf("%s.stateSyncEnabled", name),
		getOngoingSyncStateSummaryTag: fmt.Sprintf("%s.getOngoingSyncStateSummary", name),
		getLastStateSummaryTag:        fmt.Sprintf("%s.getLastStateSummary", name),
		parseStateSummaryTag:          fmt.Sprintf("%s.parseStateSummary", name),
		getStateSummaryTag:            fmt.Sprintf("%s.getStateSummary", name),
		tracer:                        tracer,
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
	ctx, span := vm.tracer.Start(ctx, vm.initializeTag)
	defer span.End()

	return vm.ChainVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		toEngine,
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
