// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/timer"
)

func TestManagerFire(t *testing.T) {
	manager := Manager{}
	manager.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout:    time.Millisecond,
		MinimumTimeout:    time.Millisecond,
		MaximumTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.1,
		TimeoutReduction:  time.Millisecond,
		Namespace:         "",
		Registerer:        prometheus.NewRegistry(),
	})
	go manager.Dispatch()

	wg := sync.WaitGroup{}
	wg.Add(1)

	manager.Register(ids.NewShortID([20]byte{}), ids.NewID([32]byte{}), 0, wg.Done)

	wg.Wait()
}

func TestManagerCancel(t *testing.T) {
	manager := Manager{}
	manager.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout:    time.Millisecond,
		MinimumTimeout:    time.Millisecond,
		MaximumTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.1,
		TimeoutReduction:  time.Millisecond,
		Namespace:         "",
		Registerer:        prometheus.NewRegistry(),
	})
	go manager.Dispatch()

	wg := sync.WaitGroup{}
	wg.Add(1)

	fired := new(bool)

	manager.Register(ids.NewShortID([20]byte{}), ids.NewID([32]byte{}), 0, func() { *fired = true })

	manager.Cancel(ids.NewShortID([20]byte{}), ids.NewID([32]byte{}), 0)

	manager.Register(ids.NewShortID([20]byte{}), ids.NewID([32]byte{}), 1, wg.Done)

	wg.Wait()

	if *fired {
		t.Fatalf("Should have cancelled the function")
	}
}
