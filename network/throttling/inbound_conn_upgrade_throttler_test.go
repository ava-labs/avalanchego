// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"net"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

var (
	host1     = utils.IPDesc{IP: net.IPv4(1, 2, 3, 4), Port: 9651}
	host2     = utils.IPDesc{IP: net.IPv4(1, 2, 3, 5), Port: 9653}
	host3     = utils.IPDesc{IP: net.IPv4(1, 2, 3, 6), Port: 9655}
	host4     = utils.IPDesc{IP: net.IPv4(1, 2, 3, 7), Port: 9657}
	localhost = utils.IPDesc{IP: net.IPv4(127, 0, 0, 1), Port: 9657}
)

func TestNoInboundConnUpgradeThrottler(t *testing.T) {
	{
		throttler := NewInboundConnUpgradeThrottler(
			logging.NoLog{},
			InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        0,
				MaxRecentConnsUpgraded: 5,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			allow := throttler.ShouldUpgrade(host1)
			assert.True(t, allow)
		}
	}
	{
		throttler := NewInboundConnUpgradeThrottler(
			logging.NoLog{},
			InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        time.Second,
				MaxRecentConnsUpgraded: 0,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			allow := throttler.ShouldUpgrade(host1)
			assert.True(t, allow)
		}
	}
}

func TestInboundConnUpgradeThrottler(t *testing.T) {
	assert := assert.New(t)

	cooldown := 5 * time.Second
	throttlerIntf := NewInboundConnUpgradeThrottler(
		logging.NoLog{},
		InboundConnUpgradeThrottlerConfig{
			UpgradeCooldown:        cooldown,
			MaxRecentConnsUpgraded: 3,
		},
	)

	// Allow should always return true
	// when called with a given IP for the first time
	assert.True(throttlerIntf.ShouldUpgrade(host1))
	assert.True(throttlerIntf.ShouldUpgrade(host2))
	assert.True(throttlerIntf.ShouldUpgrade(host3))

	// Shouldn't allow this IP because the number of connections
	// within the last [cooldown] is at [MaxRecentConns]
	assert.False(throttlerIntf.ShouldUpgrade(host4))

	// Shouldn't allow these IPs again until [cooldown] has passed
	assert.False(throttlerIntf.ShouldUpgrade(host1))
	assert.False(throttlerIntf.ShouldUpgrade(host2))
	assert.False(throttlerIntf.ShouldUpgrade(host3))

	// Local host should never be rate-limited
	assert.True(throttlerIntf.ShouldUpgrade(localhost))
	assert.True(throttlerIntf.ShouldUpgrade(localhost))
	assert.True(throttlerIntf.ShouldUpgrade(localhost))
	assert.True(throttlerIntf.ShouldUpgrade(localhost))
	assert.True(throttlerIntf.ShouldUpgrade(localhost))

	// Make sure [throttler.done] isn't closed
	throttler := throttlerIntf.(*inboundConnUpgradeThrottler)
	select {
	case <-throttler.done:
		t.Fatal("shouldn't be done")
	default:
	}

	throttler.Stop()

	// Make sure [throttler.done] is closed
	select {
	case _, chanOpen := <-throttler.done:
		assert.False(chanOpen)
	default:
		t.Fatal("should be done")
	}
}
