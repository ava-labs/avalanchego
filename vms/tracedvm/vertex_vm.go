// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"
)

var _ vertex.DAGVM = (*vertexVM)(nil)

type vertexVM struct {
	vertex.DAGVM
	tracer trace.Tracer
}

func NewVertexVM(vm vertex.DAGVM, tracer trace.Tracer) vertex.DAGVM {
	return &vertexVM{
		DAGVM:  vm,
		tracer: tracer,
	}
}

func (vm *vertexVM) Initialize(
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
	ctx, span := vm.tracer.Start(ctx, "vertexVM.Initialize")
	defer span.End()

	return vm.DAGVM.Initialize(
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

func (vm *vertexVM) PendingTxs(ctx context.Context) []snowstorm.Tx {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.PendingTxs")
	defer span.End()

	return vm.DAGVM.PendingTxs(ctx)
}

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (snowstorm.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.DAGVM.ParseTx(ctx, txBytes)
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, err
}

func (vm *vertexVM) GetTx(ctx context.Context, txID ids.ID) (snowstorm.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.GetTx", oteltrace.WithAttributes(
		attribute.Stringer("txID", txID),
	))
	defer span.End()

	tx, err := vm.DAGVM.GetTx(ctx, txID)
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, err
}
