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
	lock          sync.Mutex
	price, minFee *big.Int
}

func (m *mockGasPriceSetter) SetGasPrice(price *big.Int) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.price = price
}

func (m *mockGasPriceSetter) SetMinFee(minFee *big.Int) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.minFee = minFee
}

func (m *mockGasPriceSetter) GetStatus() (*big.Int, *big.Int) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.price, m.minFee
}

func attemptAwait(t *testing.T, wg *sync.WaitGroup, delay time.Duration) {
	ticker := make(chan struct{})

	// Wait for [wg] and then close [ticket] to indicate that
	// the wait group has finished.
	go func() {
		wg.Wait()
		close(ticker)
	}()

	select {
	case <-time.After(delay):
		t.Fatal("Timed out waiting for wait group to complete")
	case <-ticker:
		// The wait group completed without issue
	}
}

func TestUpdateGasPriceShutsDown(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	config := *params.TestChainConfig
	// Set ApricotPhase3BlockTime one hour in the future so that it will
	// create a goroutine waiting for an hour before updating the gas price
	config.ApricotPhase3BlockTimestamp = big.NewInt(time.Now().Add(time.Hour).Unix())
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  &config,
		shutdownChan: shutdownChan,
		wg:           wg,
	}

	gpu.start()
	// Close [shutdownChan] and ensure that the wait group finishes in a reasonable
	// amount of time.
	close(shutdownChan)
	attemptAwait(t, wg, 5*time.Second)
}

func TestUpdateGasPriceInitializesPrice(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  params.TestChainConfig,
		shutdownChan: shutdownChan,
		wg:           wg,
	}

	gpu.start()
	// The wait group should finish immediately since no goroutine
	// should be created when all prices should be set from the start
	attemptAwait(t, wg, time.Millisecond)

	if gpu.setter.(*mockGasPriceSetter).price.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("Expected price to match minimum base fee for apricot phase3")
	}
	if minFee := gpu.setter.(*mockGasPriceSetter).minFee; minFee == nil || minFee.Cmp(big.NewInt(params.ApricotPhase4MinBaseFee)) != 0 {
		t.Fatalf("Expected min fee to match minimum fee for apricotPhase4, but found: %d", minFee)
	}
}

func TestUpdateGasPriceUpdatesPrice(t *testing.T) {
	shutdownChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	config := *params.TestChainConfig
	// Set ApricotPhase3BlockTime 250ms in the future so that it will
	// create a goroutine waiting for the time to update the gas price
	config.ApricotPhase3BlockTimestamp = big.NewInt(time.Now().Add(250 * time.Millisecond).Unix())
	config.ApricotPhase4BlockTimestamp = big.NewInt(time.Now().Add(3 * time.Second).Unix())
	gpu := &gasPriceUpdater{
		setter:       &mockGasPriceSetter{price: big.NewInt(1)},
		chainConfig:  &config,
		shutdownChan: shutdownChan,
		wg:           wg,
	}

	gpu.start()

	// With ApricotPhase3 set slightly in the future, the gas price updater should create a
	// goroutine to sleep until its time to update and mark the wait group as done when it has
	// completed the update.
	time.Sleep(1 * time.Second)
	price, minFee := gpu.setter.(*mockGasPriceSetter).GetStatus()
	if price.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("Expected price to match minimum base fee for apricot phase3")
	}
	if minFee == nil || minFee.Cmp(big.NewInt(params.ApricotPhase3MinBaseFee)) != 0 {
		t.Fatalf("Expected min fee to match minimum fee for apricotPhase3, but found: %d", minFee)
	}

	// Confirm ApricotPhase4 settings are applied at the very end.
	attemptAwait(t, wg, 5*time.Second)
	price, minFee = gpu.setter.(*mockGasPriceSetter).GetStatus()
	if price.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("Expected price to match minimum base fee for apricot phase4")
	}
	if minFee == nil || minFee.Cmp(big.NewInt(params.ApricotPhase4MinBaseFee)) != 0 {
		t.Fatalf("Expected min fee to match minimum fee for apricotPhase4, but found: %d", minFee)
	}
}
