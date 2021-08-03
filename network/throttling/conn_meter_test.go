// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

const (
	host1 = "127.0.0.1"
	host2 = "127.0.0.2"
	host3 = "127.0.0.3"
	host4 = "127.0.0.4"
	host5 = "127.0.0.5"
)

func TestNoInboundConnThrottler(t *testing.T) {
	{
		throttler := NewInboundConnThrottler(
			logging.NoLog{},
			InboundConnThrottlerConfig{
				AllowCooldown:  0,
				MaxRecentConns: 5,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			allow := throttler.Allow(host1)
			assert.True(t, allow)
		}
	}
	{
		throttler := NewInboundConnThrottler(
			logging.NoLog{},
			InboundConnThrottlerConfig{
				AllowCooldown:  time.Second,
				MaxRecentConns: 0,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			allow := throttler.Allow(host1)
			assert.True(t, allow)
		}
	}
}

func TestInboundConnThrottler(t *testing.T) {
	cooldown := 5 * time.Second
	throttlerIntf := NewInboundConnThrottler(
		logging.NoLog{},
		InboundConnThrottlerConfig{
			AllowCooldown:  cooldown,
			MaxRecentConns: 3,
		},
	)
	go throttlerIntf.Dispatch()

	// Allow should always return true
	// when called with a given IP for the first time
	assert.True(t, throttlerIntf.Allow(host1))
	assert.True(t, throttlerIntf.Allow(host2))
	assert.True(t, throttlerIntf.Allow(host3))

	// If we didn't do the line below, there would be a race condition
	// where the number of messages on recentIPsAndTimes could be
	// MaxRecentConns or MaxRecentConns - 1. We do the line below
	// to ensure that the number of messages is MaxRecentConns
	_ = throttlerIntf.Allow(host4)
	// Shouldn't allow this IP because the number of connections
	// within the last [cooldown] is at [MaxRecentConns]
	assert.False(t, throttlerIntf.Allow(host5))

	// Shouldn't allow these IPs again until [cooldown] has passed
	assert.False(t, throttlerIntf.Allow(host1))
	assert.False(t, throttlerIntf.Allow(host2))
	assert.False(t, throttlerIntf.Allow(host3))

	// Make sure [throttler.done] isn't closed
	throttler := throttlerIntf.(*inboundConnThrottler)
	select {
	case <-throttler.done:
		t.Fatal("shouldn't be done")
	default:
	}

	throttler.Stop()

	// Make sure [throttler.done] is closed
	select {
	case _, chanOpen := <-throttler.done:
		assert.False(t, chanOpen)
	default:
		t.Fatal("should be done")
	}
}
