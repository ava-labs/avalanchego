// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolHandlesRequests(_ *testing.T) {
	var (
		poolWorkers = 5
		requests    = 15

		p = NewPool(poolWorkers)
	)

	for i := 0; i < requests; i++ {
		go func() {
			p.Send(func() {
				time.Sleep(20 * time.Second)
			})
		}()
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
