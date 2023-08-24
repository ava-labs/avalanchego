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

	_, err := NewPool(5)
	require.NoError(err)

	_, err = NewPool(0)
	require.ErrorIs(err, ErrNoWorkers)

	_, err = NewPool(-1)
	require.ErrorIs(err, ErrNoWorkers)
}

func TestPoolHandlesRequests(t *testing.T) {
	var requests []Request
	wg := &sync.WaitGroup{}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		requests = append(requests, func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
		})
	}

	p, err := NewPool(5)
	require.NoError(t, err)

	p.Start()

	for _, r := range requests {
		p.Send(r)
	}

	p.Shutdown()

	// we'll get a timeout failure if the tasks weren't processed
	wg.Wait()
}

func TestWorkerPoolMultipleOutOfOrderSendsAndStopsAreAllowed(t *testing.T) {
	require := require.New(t)
	p, err := NewPool(1)
	require.NoError(err)

	// check it's fine to send and shutdown
	// multiple times before starting the pool
	require.NotPanics(func() {
		p.Shutdown()
	})
	require.NotPanics(func() {
		p.Send(func() {})
	})
	require.NotPanics(func() {
		p.Shutdown()
	})

	// start the pool
	require.NotPanics(func() {
		p.Start()
	})

	// check that it's fine stopping the pool
	// multiple times
	require.NotPanics(func() {
		p.Shutdown()
	})
	require.NotPanics(func() {
		p.Shutdown()
	})
}
