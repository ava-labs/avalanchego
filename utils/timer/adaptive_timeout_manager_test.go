// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/prometheus/client_golang/prometheus"
)

func TestAdaptiveTimeoutManager(t *testing.T) {
	tm := AdaptiveTimeoutManager{}
	tm.Initialize(
		time.Millisecond,         // initialDuration
		2,                        // increaseRatio
		time.Microsecond,         // decreaseValue
		"gecko",                  // namespace
		prometheus.NewRegistry(), // registerer
	)
	go tm.Dispatch()

	var lock sync.Mutex

	numSuccessful := 5

	wg := sync.WaitGroup{}
	wg.Add(numSuccessful)

	callback := new(func())
	*callback = func() {
		lock.Lock()
		defer lock.Unlock()

		t.Logf("Current timeout: %s", tm.currentDuration)
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
