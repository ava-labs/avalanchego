// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestBandwidthThrottler(t *testing.T) {
	require := require.New(t)
	// Assert initial state
	config := BandwidthThrottlerConfig{
		RefillRate:   8,
		MaxBurstSize: 10,
	}
	throttlerIntf, err := newBandwidthThrottler(logging.NoLog{}, prometheus.NewRegistry(), config)
	require.NoError(err)
	require.IsType(&bandwidthThrottlerImpl{}, throttlerIntf)
	throttler := throttlerIntf.(*bandwidthThrottlerImpl)
	require.NotNil(throttler.log)
	require.NotNil(throttler.limiters)
	require.Equal(config.RefillRate, throttler.RefillRate)
	require.Equal(config.MaxBurstSize, throttler.MaxBurstSize)
	require.Empty(throttler.limiters)

	// Add a node
	nodeID1 := ids.GenerateTestNodeID()
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Remove the node
	throttler.RemoveNode(nodeID1)
	require.Empty(throttler.limiters)

	// Add the node back
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Should be able to acquire 8
	throttler.Acquire(t.Context(), 8, nodeID1)

	// Make several goroutines that acquire bytes.
	wg := sync.WaitGroup{}
	wg.Add(int(config.MaxBurstSize) + 5)
	for i := uint64(0); i < config.MaxBurstSize+5; i++ {
		go func() {
			throttler.Acquire(t.Context(), 1, nodeID1)
			wg.Done()
		}()
	}
	wg.Wait()
}
