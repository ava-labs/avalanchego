// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/api/metrics"
	"github.com/chain4travel/caminogo/database/manager"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow"
	"github.com/chain4travel/caminogo/snow/consensus/snowstorm"
	"github.com/chain4travel/caminogo/snow/engine/avalanche/vertex"
	"github.com/chain4travel/caminogo/snow/engine/common"
	"github.com/chain4travel/caminogo/utils/timer/mockable"
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
