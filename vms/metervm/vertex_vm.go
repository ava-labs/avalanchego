// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	_ vertex.DAGVM = &vertexVM{}
	_ snowstorm.Tx = &meterTx{}
)

func NewVertexVM(vm vertex.DAGVM) vertex.DAGVM {
	return &vertexVM{
		DAGVM: vm,
	}
}

type vertexVM struct {
	vertex.DAGVM
	vertexMetrics
	clock mockable.Clock
}

func (vm *vertexVM) Initialize(
	ctx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	registerer := prometheus.NewRegistry()
	if err := vm.vertexMetrics.Initialize("", registerer); err != nil {
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
	if err := ctx.Metrics.Register(multiGatherer); err != nil {
		return err
	}
	ctx.Metrics = optionalGatherer

	return vm.DAGVM.Initialize(ctx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs, appSender)
}

func (vm *vertexVM) PendingTxs() []snowstorm.Tx {
	start := vm.clock.Time()
	txs := vm.DAGVM.PendingTxs()
	end := vm.clock.Time()
	vm.vertexMetrics.pending.Observe(float64(end.Sub(start)))
	return txs
}

func (vm *vertexVM) ParseTx(b []byte) (snowstorm.Tx, error) {
	start := vm.clock.Time()
	tx, err := vm.DAGVM.ParseTx(b)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
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

func (vm *vertexVM) GetTx(txID ids.ID) (snowstorm.Tx, error) {
	start := vm.clock.Time()
	tx, err := vm.DAGVM.GetTx(txID)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.vertexMetrics.getErr.Observe(duration)
		return nil, err
	}
	vm.vertexMetrics.get.Observe(duration)
	return &meterTx{
		Tx: tx,
		vm: vm,
	}, nil
}

type meterTx struct {
	snowstorm.Tx

	vm *vertexVM
}

func (mtx *meterTx) Verify() error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Verify()
	end := mtx.vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		mtx.vm.vertexMetrics.verifyErr.Observe(duration)
	} else {
		mtx.vm.vertexMetrics.verify.Observe(duration)
	}
	return err
}

func (mtx *meterTx) Accept() error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Accept()
	end := mtx.vm.clock.Time()
	mtx.vm.vertexMetrics.accept.Observe(float64(end.Sub(start)))
	return err
}

func (mtx *meterTx) Reject() error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Reject()
	end := mtx.vm.clock.Time()
	mtx.vm.vertexMetrics.reject.Observe(float64(end.Sub(start)))
	return err
}
