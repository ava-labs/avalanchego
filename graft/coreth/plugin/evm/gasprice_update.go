// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/coreth/params"
)

type gasPriceUpdater struct {
	setter       gasPriceSetter
	chainConfig  *params.ChainConfig
	shutdownChan <-chan struct{}

	wg *sync.WaitGroup
}

type gasPriceSetter interface {
	SetGasPrice(price *big.Int)
}

// handleGasPriceUpdates creates and runs an instance of
func (vm *VM) handleGasPriceUpdates() {
	gpu := &gasPriceUpdater{
		setter:       vm.chain.GetTxPool(),
		chainConfig:  vm.chainConfig,
		shutdownChan: vm.shutdownChan,
		wg:           &vm.shutdownWg,
	}

	gpu.start()
}

func (gpu *gasPriceUpdater) start() {
	// Sets the initial gas price to the launch minimum gas price
	gpu.setter.SetGasPrice(big.NewInt(params.LaunchMinGasPrice))

	// Updates to the minimum gas price as of ApricotPhase1 if it's already in effect or starts a goroutine to enable it at the correct time
	if disabled := gpu.handleGasPriceUpdate(gpu.chainConfig.ApricotPhase1BlockTimestamp, big.NewInt(params.ApricotPhase1MinGasPrice)); disabled {
		return
	}
	// Updates to the minimum gas price as of ApricotPhase3 if it's already in effect or starts a goroutine to enable it at the correct time
	if disabled := gpu.handleGasPriceUpdate(gpu.chainConfig.ApricotPhase3BlockTimestamp, big.NewInt(0)); disabled {
		return
	}
}

// handleGasPriceUpdate handles the gas price update to occur at [timestamp]
// to update to [gasPrice]
// returns true, if the update is disabled and further forks can be skipped
func (gpu *gasPriceUpdater) handleGasPriceUpdate(timestamp *big.Int, gasPrice *big.Int) bool {
	if timestamp == nil {
		return true
	}

	currentTime := time.Now()
	upgradeTime := time.Unix(timestamp.Int64(), 0)
	if currentTime.After(upgradeTime) {
		gpu.setter.SetGasPrice(gasPrice)
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
		gpu.setter.SetGasPrice(updatedPrice)
	case <-gpu.shutdownChan:
	}
}
