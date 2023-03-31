// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
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
	throttlerIntf, err := newBandwidthThrottler(logging.NoLog{}, "", prometheus.NewRegistry(), config)
	require.NoError(err)
	throttler, ok := throttlerIntf.(*bandwidthThrottlerImpl)
	require.True(ok)
	require.NotNil(throttler.log)
	require.NotNil(throttler.limiters)
	require.EqualValues(throttler.RefillRate, 8)
	require.EqualValues(throttler.MaxBurstSize, 10)
	require.Len(throttler.limiters, 0)

	// Add a node
	nodeID1 := ids.GenerateTestNodeID()
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Remove the node
	throttler.RemoveNode(nodeID1)
	require.Len(throttler.limiters, 0)

	// Add the node back
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Should be able to acquire 8
	throttler.Acquire(context.Background(), 8, nodeID1)

	// Make several goroutines that acquire bytes.
	wg := sync.WaitGroup{}
	wg.Add(int(config.MaxBurstSize) + 5)
	for i := uint64(0); i < config.MaxBurstSize+5; i++ {
		go func() {
			throttler.Acquire(context.Background(), 1, nodeID1)
			wg.Done()
		}()
	}
	wg.Wait()
}
