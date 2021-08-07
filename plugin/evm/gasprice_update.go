// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
)

type gasPriceUpdater struct {
	txPool       *core.TxPool
	chainConfig  *params.ChainConfig
	shutdownChan <-chan struct{}

	wg *sync.WaitGroup
}

// handleGasPriceUpdates creates and runs an instance of
func (vm *VM) handleGasPriceUpdates() {
	gpu := &gasPriceUpdater{
		txPool:       vm.chain.GetTxPool(),
		chainConfig:  vm.chainConfig,
		shutdownChan: vm.shutdownChan,
		wg:           &vm.shutdownWg,
	}

	gpu.start()
}

func (gpu *gasPriceUpdater) start() {
	// Sets the initial gas price to the launch minimum gas price
	gpu.txPool.SetGasPrice(params.LaunchMinGasPrice)

	// Updates to the minimum gas price as of ApricotPhase1 or starts a goroutine to enable it at the right time
	if disabled := gpu.handleGasPriceUpdate(gpu.chainConfig.ApricotPhase1BlockTimestamp, params.ApricotPhase1MinGasPrice); disabled {
		return
	}
	// Updates to the minimum gas price as of ApricotPhase3 or starts a goroutine to enable it at the right time
	if disabled := gpu.handleGasPriceUpdate(gpu.chainConfig.ApricotPhase3BlockTimestamp, big.NewInt(params.ApricotPhase3MinBaseFee)); disabled {
		return
	}
}

// handleGasPriceUpdate handles the gas price update to occur at [timestamp]
// to update to [gasPrice]
// returns true, if the update is not enabled and further forks can be skipped
func (gpu *gasPriceUpdater) handleGasPriceUpdate(timestamp *big.Int, gasPrice *big.Int) bool {
	if timestamp == nil {
		return true
	}

	currentTime := time.Now()
	upgradeTime := time.Unix(gpu.chainConfig.ApricotPhase1BlockTimestamp.Int64(), 0)
	if currentTime.After(upgradeTime) {
		gpu.txPool.SetGasPrice(gasPrice)
	} else {
		gpu.wg.Add(1)
		go gpu.updateGasPrice(time.Until(upgradeTime), gasPrice)
	}
	return false
}

func (gpu *gasPriceUpdater) updateGasPrice(duration time.Duration, updatedPrice *big.Int) {
	defer gpu.wg.Done()
	select {
	case <-time.After(duration):
		gpu.txPool.SetGasPrice(updatedPrice)
	case <-gpu.shutdownChan:
	}
}
