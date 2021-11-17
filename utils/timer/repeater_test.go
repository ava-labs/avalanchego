// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"
)

func TestRepeater(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(2)

	val := new(int)
	repeater := NewRepeater(func() {
		if *val < 2 {
			wg.Done()
			*val++
		}
	}, time.Millisecond)
	go repeater.Dispatch()

	wg.Wait()
	repeater.Stop()
}
