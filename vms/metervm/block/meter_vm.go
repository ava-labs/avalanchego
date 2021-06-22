// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	latencyMetrics "github.com/ava-labs/avalanchego/utils/metrics"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var _ block.ChainVM = &MeterVM{}

func NewMeterVM(vm block.ChainVM) block.ChainVM {
	return &MeterVM{
		ChainVM: vm,
	}
}

type metrics struct {
	buildBlock,
	parseBlock,
	getBlock,
	setPreference,
	lastAccepted prometheus.Histogram
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.buildBlock = latencyMetrics.NewNanosecnodsLatencyMetric(namespace, "build_block")
	m.parseBlock = latencyMetrics.NewNanosecnodsLatencyMetric(namespace, "parse_block")
	m.getBlock = latencyMetrics.NewNanosecnodsLatencyMetric(namespace, "get_block")
	m.setPreference = latencyMetrics.NewNanosecnodsLatencyMetric(namespace, "set_preference")
	m.lastAccepted = latencyMetrics.NewNanosecnodsLatencyMetric(namespace, "last_accepted")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.buildBlock),
		registerer.Register(m.parseBlock),
		registerer.Register(m.getBlock),
		registerer.Register(m.setPreference),
		registerer.Register(m.lastAccepted),
	)
	return errs.Err
}

type MeterVM struct {
	block.ChainVM
	metrics
	clock timer.Clock
}

func (vm *MeterVM) Initialize(
	ctx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	if err := vm.metrics.Initialize(fmt.Sprintf("metervm_%s", ctx.Namespace), ctx.Metrics); err != nil {
		return err
	}

	return vm.ChainVM.Initialize(ctx, db, genesisBytes, upgradeBytes, configBytes, toEngine, fxs)
}

func (vm *MeterVM) BuildBlock() (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.BuildBlock()
	end := vm.clock.Time()
	vm.metrics.buildBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *MeterVM) ParseBlock(b []byte) (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.ParseBlock(b)
	end := vm.clock.Time()
	vm.metrics.parseBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *MeterVM) GetBlock(id ids.ID) (snowman.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.GetBlock(id)
	end := vm.clock.Time()
	vm.metrics.getBlock.Observe(float64(end.Sub(start)))
	return blk, err
}

func (vm *MeterVM) SetPreference(id ids.ID) error {
	start := vm.clock.Time()
	err := vm.ChainVM.SetPreference(id)
	end := vm.clock.Time()
	vm.metrics.setPreference.Observe(float64(end.Sub(start)))
	return err
}

func (vm *MeterVM) LastAccepted() (ids.ID, error) {
	start := vm.clock.Time()
	lastAcceptedID, err := vm.ChainVM.LastAccepted()
	end := vm.clock.Time()
	vm.metrics.lastAccepted.Observe(float64(end.Sub(start)))
	return lastAcceptedID, err
}
