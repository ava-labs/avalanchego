package throttling

import (
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

func TestBandwidthThrottler(t *testing.T) {
	assert := assert.New(t)
	// Assert initial state
	config := BandwidthThrottlerConfig{
		RefillRate:   8,
		MaxBurstSize: 10,
	}
	throttlerIntf := NewBandwidthThrottler(logging.NoLog{}, config)
	throttler, ok := throttlerIntf.(*bandwidthThrottler)
	assert.True(ok)
	assert.NotNil(throttler.log)
	assert.NotNil(throttler.limiters)
	assert.EqualValues(throttler.RefillRate, 8)
	assert.EqualValues(throttler.MaxBurstSize, 10)
	assert.Len(throttler.limiters, 0)

	// Add a node
	nodeID1 := ids.GenerateTestShortID()
	throttler.AddNode(nodeID1)
	assert.Len(throttler.limiters, 1)

	// Remove the node
	throttler.RemoveNode(nodeID1)
	assert.Len(throttler.limiters, 0)

	// Add the node back
	throttler.AddNode(nodeID1)
	assert.Len(throttler.limiters, 1)

	// Should be able to acquire 8
	throttler.Acquire(8, nodeID1)

	// Make several goroutines that acquire bytes.
	wg := sync.WaitGroup{}
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			throttler.Acquire(1, nodeID1)
			wg.Done()
		}()
	}
	wg.Wait()
}
