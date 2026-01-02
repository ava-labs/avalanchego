// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"sync"
)

// Manages tracking and shutdown of VM runtimes.
type manager struct {
	lock     sync.Mutex
	runtimes []Stopper
}

// NewManager returns manager of VM runtimes.
//
// TODO: If a runtime exits before the call to `manager.Stop`, it would be nice
// to remove it from the current set.
func NewManager() Manager {
	return &manager{}
}

func (m *manager) Stop(ctx context.Context) {
	var wg sync.WaitGroup
	m.lock.Lock()
	defer func() {
		m.lock.Unlock()
		wg.Wait()
	}()

	wg.Add(len(m.runtimes))
	for _, rt := range m.runtimes {
		go func(runtime Stopper) {
			defer wg.Done()
			runtime.Stop(ctx)
		}(rt)
	}
	m.runtimes = nil
}

func (m *manager) TrackRuntime(runtime Stopper) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.runtimes = append(m.runtimes, runtime)
}
