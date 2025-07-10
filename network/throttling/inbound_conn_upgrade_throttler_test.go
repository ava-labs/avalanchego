// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	host1      = netip.AddrPortFrom(netip.AddrFrom4([4]byte{1, 2, 3, 4}), 9651)
	host2      = netip.AddrPortFrom(netip.AddrFrom4([4]byte{1, 2, 3, 5}), 9653)
	host3      = netip.AddrPortFrom(netip.AddrFrom4([4]byte{1, 2, 3, 6}), 9655)
	host4      = netip.AddrPortFrom(netip.AddrFrom4([4]byte{1, 2, 3, 7}), 9657)
	loopbackIP = netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 9657)
)

func TestNoInboundConnUpgradeThrottler(t *testing.T) {
	require := require.New(t)

	{
		throttler := NewInboundConnUpgradeThrottler(
			InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        0,
				MaxRecentConnsUpgraded: 5,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			require.True(throttler.ShouldUpgrade(host1))
		}
	}
	{
		throttler := NewInboundConnUpgradeThrottler(
			InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        time.Second,
				MaxRecentConnsUpgraded: 0,
			},
		)
		// throttler should allow all
		for i := 0; i < 10; i++ {
			require.True(throttler.ShouldUpgrade(host1))
		}
	}
}

func TestInboundConnUpgradeThrottler(t *testing.T) {
	require := require.New(t)

	cooldown := 5 * time.Second
	throttlerIntf := NewInboundConnUpgradeThrottler(
		InboundConnUpgradeThrottlerConfig{
			UpgradeCooldown:        cooldown,
			MaxRecentConnsUpgraded: 3,
		},
	)

	// Allow should always return true
	// when called with a given IP for the first time
	require.True(throttlerIntf.ShouldUpgrade(host1))
	require.True(throttlerIntf.ShouldUpgrade(host2))
	require.True(throttlerIntf.ShouldUpgrade(host3))

	// Shouldn't allow this IP because the number of connections
	// within the last [cooldown] is at [MaxRecentConns]
	require.False(throttlerIntf.ShouldUpgrade(host4))

	// Shouldn't allow these IPs again until [cooldown] has passed
	require.False(throttlerIntf.ShouldUpgrade(host1))
	require.False(throttlerIntf.ShouldUpgrade(host2))
	require.False(throttlerIntf.ShouldUpgrade(host3))

	// Local host should never be rate-limited
	require.True(throttlerIntf.ShouldUpgrade(loopbackIP))
	require.True(throttlerIntf.ShouldUpgrade(loopbackIP))
	require.True(throttlerIntf.ShouldUpgrade(loopbackIP))
	require.True(throttlerIntf.ShouldUpgrade(loopbackIP))
	require.True(throttlerIntf.ShouldUpgrade(loopbackIP))

	// Make sure [throttler.done] isn't closed
	throttler := throttlerIntf.(*inboundConnUpgradeThrottler)
	select {
	case <-throttler.done:
		require.FailNow("shouldn't be done")
	default:
	}

	throttler.Stop()

	// Make sure [throttler.done] is closed
	select {
	case _, chanOpen := <-throttler.done:
		require.False(chanOpen)
	default:
		require.FailNow("should be done")
	}
}
