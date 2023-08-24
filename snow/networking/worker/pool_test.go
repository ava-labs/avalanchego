// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolChecksWorkerCount(t *testing.T) {
	require := require.New(t)
	_, err := NewPool(-1)
	require.ErrorIs(err, ErrNoWorkers)
}

func TestPoolHandlesRequests(t *testing.T) {
	require := require.New(t)

	var (
		poolWorkers = 1
		requests    = 1
	)

	p, err := NewPool(poolWorkers)
	require.NoError(err)

	// Let's send a bunch of concurrent requests. Some of them
	// are long enough so that Shutdown is likely called while
	// they are executing
	wg := &sync.WaitGroup{}
	for i := 0; i < requests; i++ {
		shortRequest := i%2 == 0
		if shortRequest {
			wg.Add(1)
		}

		go func() {
			p.Send(func() {
				if shortRequest {
					wg.Done()
				} else {
					time.Sleep(time.Minute)
				}
			})
		}()
	}

	wg.Wait()
	p.Shutdown()
}

func TestWorkerPoolMultipleOutOfOrderSendsAndStopsAreAllowed(t *testing.T) {
	require := require.New(t)
	p, err := NewPool(1)
	require.NoError(err)

	// Shutdown is idempotent
	require.NotPanics(func() {
		p.Shutdown()
	})
	require.NotPanics(func() {
		p.Shutdown()
	})

	// late requests, after Shutdown, are no-ops that won't panic
	lateRequest := func() {
		time.Sleep(time.Minute)
	}
	require.NotPanics(func() {
		p.Send(lateRequest)
	})
	require.NotPanics(func() {
		p.Send(lateRequest)
	})
}
