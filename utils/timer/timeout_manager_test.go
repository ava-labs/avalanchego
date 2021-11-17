// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

func TestTimeoutManager(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(2)
	defer wg.Wait()

	tm := TimeoutManager{}
	tm.Initialize(time.Millisecond)
	go tm.Dispatch()

	tm.Put(ids.ID{}, wg.Done)
	tm.Put(ids.ID{1}, wg.Done)
}
