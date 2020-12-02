// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/utils/timer"
)

func TestManagerFire(t *testing.T) {
	manager := Manager{}
	benchlist := benchlist.NewNoBenchlist()
	err := manager.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout: time.Millisecond,
		MinimumTimeout: time.Millisecond,
		MaximumTimeout: 10 * time.Second,
		TimeoutInc:     2 * time.Millisecond,
		TimeoutDec:     time.Millisecond,
		Namespace:      "",
		Registerer:     prometheus.NewRegistry(),
	}, benchlist)
	if err != nil {
		t.Fatal(err)
	}
	go manager.Dispatch()

	wg := sync.WaitGroup{}
	wg.Add(1)

	manager.Register(ids.NewShortID([20]byte{}), ids.ID{}, 0, true, 0, wg.Done)

	wg.Wait()
}

func TestManagerCancel(t *testing.T) {
	manager := Manager{}
	benchlist := benchlist.NewNoBenchlist()
	err := manager.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout: time.Millisecond,
		MinimumTimeout: time.Millisecond,
		MaximumTimeout: 10 * time.Second,
		TimeoutInc:     2 * time.Millisecond,
		TimeoutDec:     time.Millisecond,
		Namespace:      "",
		Registerer:     prometheus.NewRegistry(),
	}, benchlist)
	if err != nil {
		t.Fatal(err)
	}
	go manager.Dispatch()

	wg := sync.WaitGroup{}
	wg.Add(1)

	fired := new(bool)

	manager.Register(ids.NewShortID([20]byte{}), ids.ID{}, 0, true, 0, func() { *fired = true })

	manager.Cancel(ids.NewShortID([20]byte{}), ids.ID{}, 0)

	manager.Register(ids.NewShortID([20]byte{}), ids.ID{}, 1, true, 0, wg.Done)

	wg.Wait()

	if *fired {
		t.Fatalf("Should have cancelled the function")
	}
}
