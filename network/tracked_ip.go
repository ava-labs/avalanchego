// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"math/rand"
	"net/netip"
	"sync"
	"time"
)

type trackedIP struct {
	delayLock sync.RWMutex
	delay     time.Duration

	ip netip.AddrPort

	stopTrackingOnce sync.Once
	onStopTracking   chan struct{}
}

func newTrackedIP(ip netip.AddrPort) *trackedIP {
	return &trackedIP{
		ip:             ip,
		onStopTracking: make(chan struct{}),
	}
}

func (ip *trackedIP) trackNewIP(newIP netip.AddrPort) *trackedIP {
	ip.stopTracking()
	return &trackedIP{
		delay:          ip.getDelay(),
		ip:             newIP,
		onStopTracking: make(chan struct{}),
	}
}

func (ip *trackedIP) getDelay() time.Duration {
	ip.delayLock.RLock()
	delay := ip.delay
	ip.delayLock.RUnlock()
	return delay
}

func (ip *trackedIP) increaseDelay(initialDelay, maxDelay time.Duration) {
	ip.delayLock.Lock()
	defer ip.delayLock.Unlock()

	// If the timeout was previously 0, ensure that there is a reasonable delay.
	if ip.delay <= 0 {
		ip.delay = initialDelay
	}

	// Randomization is only performed here to distribute reconnection
	// attempts to a node that previously shut down. This doesn't
	// require cryptographically secure random number generation.
	// set the timeout to [1, 2) * timeout
	ip.delay = time.Duration(float64(ip.delay) * (1 + rand.Float64())) // #nosec G404
	if ip.delay > maxDelay {
		// set the timeout to [.75, 1) * maxDelay
		ip.delay = time.Duration(float64(maxDelay) * (3 + rand.Float64()) / 4) // #nosec G404
	}
}

func (ip *trackedIP) stopTracking() {
	ip.stopTrackingOnce.Do(func() {
		close(ip.onStopTracking)
	})
}
