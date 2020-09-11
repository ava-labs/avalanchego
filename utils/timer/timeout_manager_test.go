// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-go/ids"
)

func TestTimeoutManager(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(2)
	defer wg.Wait()

	tm := TimeoutManager{}
	tm.Initialize(time.Millisecond)
	go tm.Dispatch()

	tm.Put(ids.NewID([32]byte{}), wg.Done)
	tm.Put(ids.NewID([32]byte{1}), wg.Done)
}
