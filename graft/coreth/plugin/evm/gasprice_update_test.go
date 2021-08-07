// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/coreth/params"
)

type mockGasPriceSetter struct {
	lock  sync.Mutex
	price *big.Int
}

func (m *mockGasPriceSetter) SetGasPrice(price *big.Int) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.price = price
}

func TestUpdateGasPriceShutsDown(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := sync.WaitGroup{}
	config := *params.TestChainConfig
	// Set ApricotPhase3BlockTime one hour in the future so that it will
	// create a goroutine waiting for an hour before updating the gas price
	config.ApricotPhase3BlockTimestamp = big.NewInt(time.Now().Add(time.Hour).Unix())
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  &config,
		shutdownChan: shutdownChan,
		wg:           &wg,
	}

	gpu.start()
	close(shutdownChan)
	wg.Wait()
}

func TestUpdateGasPriceInitializesPrice(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := sync.WaitGroup{}
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  params.TestChainConfig,
		shutdownChan: shutdownChan,
		wg:           &wg,
	}

	gpu.start()
	// The wait group should finish immediately since no goroutine
	// should be created when all prices should be set from the start
	wg.Wait()

	if gpu.setter.(*mockGasPriceSetter).price.Cmp(big.NewInt(params.ApricotPhase3MinBaseFee)) != 0 {
		t.Fatalf("Expected price to match minimum base fee for apricot phase3")
	}
}

func TestUpdateGasPriceUpdatesPrice(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := sync.WaitGroup{}
	config := *params.TestChainConfig
	// Set ApricotPhase3BlockTime one hour in the future so that it will
	// create a goroutine waiting for the time to update the gas price
	config.ApricotPhase3BlockTimestamp = big.NewInt(time.Now().Add(250 * time.Millisecond).Unix())
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  &config,
		shutdownChan: shutdownChan,
		wg:           &wg,
	}

	gpu.start()
	// With ApricotPhase3 set slightly in the future, the gas price updater should create a
	// goroutine to sleep until its time to update and mark the wait group as done when it has
	// completed the update.
	wg.Wait()

	if gpu.setter.(*mockGasPriceSetter).price.Cmp(big.NewInt(params.ApricotPhase3MinBaseFee)) != 0 {
		t.Fatalf("Expected price to match minimum base fee for apricot phase3")
	}
}
