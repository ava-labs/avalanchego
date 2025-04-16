// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var _ vertex.LinearizableVMWithEngine = (*vertexVM)(nil)

type vertexVM struct {
	vertex.LinearizableVMWithEngine
	tracer trace.Tracer
}

func NewVertexVM(vm vertex.LinearizableVMWithEngine, tracer trace.Tracer) vertex.LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		tracer:                   tracer,
	}
}

func (vm *vertexVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.Initialize")
	defer span.End()

	return vm.LinearizableVMWithEngine.Initialize(
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

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (snowstorm.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, txBytes)
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, err
}
