// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"net/netip"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
)

var (
	_ InboundConnUpgradeThrottler = (*inboundConnUpgradeThrottler)(nil)
	_ InboundConnUpgradeThrottler = (*noInboundConnUpgradeThrottler)(nil)
)

// InboundConnUpgradeThrottler returns whether we should upgrade an inbound connection from IP [ipStr].
// If ShouldUpgrade(ipStr) returns false, the connection to that IP should be closed.
// Note that InboundConnUpgradeThrottler rate-limits _upgrading_ of
// inbound connections, whereas throttledListener rate-limits
// _acceptance_ of inbound connections.
type InboundConnUpgradeThrottler interface {
	// Dispatch starts this InboundConnUpgradeThrottler.
	// Must be called before [ShouldUpgrade].
	// Blocks until [Stop] is called (i.e. should be called in a goroutine.)
	Dispatch()
	// Stop this InboundConnUpgradeThrottler and causes [Dispatch] to return.
	// Should be called when we're done with this InboundConnUpgradeThrottler.
	// This InboundConnUpgradeThrottler must not be used after [Stop] is called.
	Stop()
	// Returns whether we should upgrade an inbound connection from [ipStr].
	// Must only be called after [Dispatch] has been called.
	// If [ip] is a local IP, this method always returns true.
	// Must not be called after [Stop] has been called.
	ShouldUpgrade(ip netip.AddrPort) bool
}

type InboundConnUpgradeThrottlerConfig struct {
	// ShouldUpgrade(ipStr) returns true if it has been at least [UpgradeCooldown]
	// since the last time ShouldUpgrade(ipStr) returned true or if
	// ShouldUpgrade(ipStr) has never been called.
	// If <= 0, inbound connections not rate-limited.
	UpgradeCooldown time.Duration `json:"upgradeCooldown"`
	// Maximum number of inbound connections upgraded within [UpgradeCooldown].
	// (As implemented in inboundConnUpgradeThrottler, may actually upgrade
	// [MaxRecentConnsUpgraded+1] due to a race condition but that's fine.)
	// If <= 0, inbound connections not rate-limited.
	MaxRecentConnsUpgraded int `json:"maxRecentConnsUpgraded"`
}

// Returns an InboundConnUpgradeThrottler that upgrades an inbound
// connection from a given IP at most every [UpgradeCooldown].
func NewInboundConnUpgradeThrottler(config InboundConnUpgradeThrottlerConfig) InboundConnUpgradeThrottler {
	if config.UpgradeCooldown <= 0 || config.MaxRecentConnsUpgraded <= 0 {
		return &noInboundConnUpgradeThrottler{}
	}
	return &inboundConnUpgradeThrottler{
		InboundConnUpgradeThrottlerConfig: config,
		done:                              make(chan struct{}),
		recentIPsAndTimes:                 make(chan ipAndTime, config.MaxRecentConnsUpgraded),
	}
}

// noInboundConnUpgradeThrottler upgrades all inbound connections
type noInboundConnUpgradeThrottler struct{}

func (*noInboundConnUpgradeThrottler) Dispatch() {}

func (*noInboundConnUpgradeThrottler) Stop() {}

func (*noInboundConnUpgradeThrottler) ShouldUpgrade(netip.AddrPort) bool {
	return true
}

type ipAndTime struct {
	ip                netip.Addr
	cooldownElapsedAt time.Time
}

type inboundConnUpgradeThrottler struct {
	InboundConnUpgradeThrottlerConfig

	lock sync.Mutex
	// Useful for faking time in tests
	clock mockable.Clock
	// When [done] is closed, Dispatch returns.
	done chan struct{}
	// IP --> Present if ShouldUpgrade(ipStr) returned true
	// within the last [UpgradeCooldown].
	recentIPs set.Set[netip.Addr]
	// Sorted in order of increasing time
	// of last call to ShouldUpgrade that returned true.
	// For each IP in this channel, ShouldUpgrade(ipStr)
	// returned true within the last [UpgradeCooldown].
	recentIPsAndTimes chan ipAndTime
}

// Returns whether we should upgrade an inbound connection from [ipStr].
func (n *inboundConnUpgradeThrottler) ShouldUpgrade(addrPort netip.AddrPort) bool {
	// Only use addr (not port). This mitigates DoS attacks from many nodes on one
	// host.
	addr := addrPort.Addr()
	if addr.IsLoopback() {
		// Don't rate-limit loopback IPs
		return true
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	if n.recentIPs.Contains(addr) {
		// We recently upgraded an inbound connection from this IP
		return false
	}

	select {
	case n.recentIPsAndTimes <- ipAndTime{
		ip:                addr,
		cooldownElapsedAt: n.clock.Time().Add(n.UpgradeCooldown),
	}:
		n.recentIPs.Add(addr)
		return true
	default:
		return false
	}
}

func (n *inboundConnUpgradeThrottler) Dispatch() {
	timer := timerpkg.StoppedTimer()

	defer timer.Stop()
	for {
		select {
		case next := <-n.recentIPsAndTimes:
			// Sleep until it's time to remove the next IP
			timer.Reset(next.cooldownElapsedAt.Sub(n.clock.Time()))

			select {
			case <-timer.C:
				// Remove the next IP (we'd upgrade another inbound connection from it)
				n.lock.Lock()
				n.recentIPs.Remove(next.ip)
				n.lock.Unlock()
			case <-n.done:
				return
			}
		case <-n.done:
			return
		}
	}
}

func (n *inboundConnUpgradeThrottler) Stop() {
	close(n.done)
}
