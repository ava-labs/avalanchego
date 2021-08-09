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
	SetMinFee(price *big.Int)
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

// start handles the appropriate gas price and minimum fee updates required by [gpu.chainConfig]
func (gpu *gasPriceUpdater) start() {
	// Sets the initial gas price to the launch minimum gas price
	gpu.setter.SetGasPrice(big.NewInt(params.LaunchMinGasPrice))

	// Updates to the minimum gas price as of ApricotPhase1 if it's already in effect or starts a goroutine to enable it at the correct time
	if disabled := gpu.handleUpdate(gpu.setter.SetGasPrice, gpu.chainConfig.ApricotPhase1BlockTimestamp, big.NewInt(params.ApricotPhase1MinGasPrice)); disabled {
		return
	}
	// Updates to the minimum gas price as of ApricotPhase3 if it's already in effect or starts a goroutine to enable it at the correct time
	if disabled := gpu.handleUpdate(gpu.setter.SetGasPrice, gpu.chainConfig.ApricotPhase3BlockTimestamp, big.NewInt(0)); disabled {
		return
	}
	gpu.handleUpdate(gpu.setter.SetMinFee, gpu.chainConfig.ApricotPhase3BlockTimestamp, big.NewInt(params.ApricotPhase3MinBaseFee))
}

// handleUpdate handles calling update(price) at the appropriate time based on
// the value of [timestamp].
// 1) If [timestamp] is nil, update is never called
// 2) If [timestamp] has already passed, update is called immediately
// 3) [timestamp] is some time in the future, starts a goroutine that will call update(price) at the time
// given by [timestamp].
func (gpu *gasPriceUpdater) handleUpdate(update func(price *big.Int), timestamp *big.Int, price *big.Int) bool {
	if timestamp == nil {
		return true
	}

	currentTime := time.Now()
	upgradeTime := time.Unix(timestamp.Int64(), 0)
	if currentTime.After(upgradeTime) {
		update(price)
	} else {
		gpu.wg.Add(1)
		go gpu.updatePrice(update, time.Until(upgradeTime), price)
	}
	return false
}

// updatePrice calls update(updatedPrice) after waiting for [duration] or shuts down early
// if the [shutdownChan] is closed.
func (gpu *gasPriceUpdater) updatePrice(update func(price *big.Int), duration time.Duration, updatedPrice *big.Int) {
	defer gpu.wg.Done()
	select {
	case <-time.After(duration):
		update(updatedPrice)
	case <-gpu.shutdownChan:
	}
}
