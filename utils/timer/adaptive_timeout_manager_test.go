// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/constants"
)

func TestAdaptiveTimeoutManager(t *testing.T) {
	tm := AdaptiveTimeoutManager{}
	tm.Initialize(&AdaptiveTimeoutConfig{
		InitialTimeout:    time.Millisecond,
		MinimumTimeout:    time.Millisecond,
		MaximumTimeout:    time.Hour,
		TimeoutMultiplier: 2,
		TimeoutReduction:  time.Microsecond,
		Namespace:         constants.PlatformName,
		Registerer:        prometheus.NewRegistry(),
	})
	go tm.Dispatch()

	var lock sync.Mutex

	numSuccessful := 5

	wg := sync.WaitGroup{}
	wg.Add(numSuccessful)

	callback := new(func())
	*callback = func() {
		lock.Lock()
		defer lock.Unlock()

		numSuccessful--
		if numSuccessful > 0 {
			tm.Put(ids.NewID([32]byte{byte(numSuccessful)}), *callback)
		}
		if numSuccessful >= 0 {
			wg.Done()
		}
		if numSuccessful%2 == 0 {
			tm.Remove(ids.NewID([32]byte{byte(numSuccessful)}))
			tm.Put(ids.NewID([32]byte{byte(numSuccessful)}), *callback)
		}
	}
	(*callback)()
	(*callback)()

	wg.Wait()
}
