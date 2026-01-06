// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "sync"

// waitGroup is a small wrapper of a sync.WaitGroup that avoids performing a
// memory allocation when Add is never called.
type waitGroup struct {
	wg *sync.WaitGroup
}

func (wg *waitGroup) Add(delta int) {
	if wg.wg == nil {
		wg.wg = new(sync.WaitGroup)
	}
	wg.wg.Add(delta)
}

func (wg *waitGroup) Wait() {
	if wg.wg != nil {
		wg.wg.Wait()
	}
}
