// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPoolHandlesRequests(t *testing.T) {
	var (
		poolWorkers = 5
		requests    = 5

		jobsDone = make([]bool, requests)

		p = NewPool(poolWorkers)
	)

	wg := sync.WaitGroup{}
	for i := range jobsDone {
		wg.Add(1)
		idx := i
		go func() {
			p.Send(func() {
				jobsDone[idx] = true
				wg.Done()
			})
		}()
	}

	wg.Wait()
	for i, res := range jobsDone {
		require.True(t, res, fmt.Sprintf("job %v not done", i))
	}
	p.Shutdown()
}

func TestWorkerPoolMultipleOutOfOrderSendsAndStopsAreAllowed(t *testing.T) {
	require := require.New(t)
	p := NewPool(1)

	// Shutdown is idempotent
	require.NotPanics(func() {
		p.Shutdown()
	})
	require.NotPanics(func() {
		p.Shutdown()
	})

	// late requests, after Shutdown, are no-ops that won't panic
	jobDone := false
	lateRequest := func() {
		jobDone = true
	}
	require.NotPanics(func() {
		p.Send(lateRequest)
	})
	require.False(jobDone)

	require.NotPanics(func() {
		p.Send(lateRequest)
	})
	require.False(jobDone)
}
