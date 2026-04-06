// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeouttest

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// New creates a new timeout Manager for testing using the given benchlist
// manager and initial timeout duration. The timeout manager is automatically
// cleaned up when the test finishes.
func New(t testing.TB, benchlistMgr benchlist.Manager, initialTimeout time.Duration) *timeout.Manager {
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     initialTimeout,
			MinimumTimeout:     initialTimeout,
			MaximumTimeout:     10 * time.Second,
			TimeoutCoefficient: 1.25,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlistMgr,
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	go tm.Dispatch()
	t.Cleanup(tm.Stop)

	return tm
}
