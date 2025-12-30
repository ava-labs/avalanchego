// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ vertex.LinearizableVMWithEngine = (*vertexVM)(nil)
	_ snowstorm.Tx                    = (*meterTx)(nil)
)

func NewVertexVM(
	vm vertex.LinearizableVMWithEngine,
	reg prometheus.Registerer,
) vertex.LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		registry:                 reg,
	}
}

type vertexVM struct {
	vertex.LinearizableVMWithEngine
	vertexMetrics
	registry prometheus.Registerer
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
	if err := vm.vertexMetrics.Initialize(vm.registry); err != nil {
		return err
	}

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

func (vm *vertexVM) ParseTx(ctx context.Context, b []byte) (snowstorm.Tx, error) {
	start := time.Now()
	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, b)
	duration := float64(time.Since(start))
	if err != nil {
		vm.vertexMetrics.parseErr.Observe(duration)
		return nil, err
	}
	vm.vertexMetrics.parse.Observe(duration)
	return &meterTx{
		Tx: tx,
		vm: vm,
	}, nil
}

type meterTx struct {
	snowstorm.Tx

	vm *vertexVM
}

func (mtx *meterTx) Verify(ctx context.Context) error {
	start := time.Now()
	err := mtx.Tx.Verify(ctx)
	duration := float64(time.Since(start))
	if err != nil {
		mtx.vm.vertexMetrics.verifyErr.Observe(duration)
	} else {
		mtx.vm.vertexMetrics.verify.Observe(duration)
	}
	return err
}

func (mtx *meterTx) Accept(ctx context.Context) error {
	start := time.Now()
	err := mtx.Tx.Accept(ctx)
	mtx.vm.vertexMetrics.accept.Observe(float64(time.Since(start)))
	return err
}

func (mtx *meterTx) Reject(ctx context.Context) error {
	start := time.Now()
	err := mtx.Tx.Reject(ctx)
	mtx.vm.vertexMetrics.reject.Observe(float64(time.Since(start)))
	return err
}
