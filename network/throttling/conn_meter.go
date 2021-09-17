// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

var (
	_ InboundConnThrottler = &inboundConnThrottler{}
	_ InboundConnThrottler = &noInboundConnThrottler{}
)

// InboundConnThrottler returns whether we should upgrade an inbound connection from IP [ipStr].
// If ShouldUpgrade(ipStr) returns false, the connection to that IP should be closed.
type InboundConnThrottler interface {
	// Dispatch starts this InboundConnThrottler.
	// Must be called before [ShouldUpgrade].
	// Blocks until [Stop] is called (i.e. should be called in a goroutine.)
	Dispatch()
	// Stop this InboundConnThrottler and causes [Dispatch] to return.
	// Should be called when we're done with this InboundConnThrottler.
	// This InboundConnThrottler must not be used after [Stop] is called.
	Stop()
	// Returns whether we should upgrade an inbound connection from [ipStr].
	// Must only be called after [Dispatch] has been called.
	// Must not be called after [Stop] has been called.
	ShouldUpgrade(ipStr string) bool
}

type InboundConnUpgradeThrottlerConfig struct {
	// ShouldUpgrade(ipStr) returns true if it has been at least [UpgradeCooldown]
	// since the last time ShouldUpgrade(ipStr) returned true or if
	// ShouldUpgrade(ipStr) has never been called.
	// If <= 0, inbound connections not rate-limited.
	UpgradeCooldown time.Duration `json:"upgradeCooldown"`
	// Maximum number of inbound connections upgraded within [UpgradeCooldown].
	// (As implemented in inboundConnThrottler, may actually upgrade
	// [MaxRecentConns+1] due to a race condition but that's fine.)
	// If <= 0, inbound connections not rate-limited.
	MaxRecentConnsUpgraded int `json:"maxRecentConnsUpgraded"`
}

// Returns an InboundConnThrottler that upgrades an inbound
// connection from a given IP at most every [UpgradeCooldown].
func NewInboundConnThrottler(log logging.Logger, config InboundConnUpgradeThrottlerConfig) InboundConnThrottler {
	if config.UpgradeCooldown <= 0 || config.MaxRecentConnsUpgraded <= 0 {
		return &noInboundConnThrottler{}
	}
	return &inboundConnThrottler{
		InboundConnUpgradeThrottlerConfig: config,
		log:                               log,
		done:                              make(chan struct{}),
		recentIPs:                         make(map[string]struct{}),
		recentIPsAndTimes:                 make(chan ipAndTime, config.MaxRecentConnsUpgraded),
	}
}

// noInboundConnThrottler upgrades all inbound connections
type noInboundConnThrottler struct{}

func (*noInboundConnThrottler) Dispatch()                 {}
func (*noInboundConnThrottler) Stop()                     {}
func (*noInboundConnThrottler) ShouldUpgrade(string) bool { return true }

type ipAndTime struct {
	ip                string
	cooldownElapsedAt time.Time
}

// inboundConnThrottler implements InboundConnThrottler
type inboundConnThrottler struct {
	InboundConnUpgradeThrottlerConfig
	log  logging.Logger
	lock sync.Mutex
	// Useful for faking time in tests
	clock timer.Clock
	// When [done] is closed, Dispatch returns.
	done chan struct{}
	// IP --> Present if ShouldUpgrade(ipStr) returned true
	// within the last [UpgradeCooldown].
	recentIPs map[string]struct{}
	// Sorted in order of increasing time
	// of last call to ShouldUpgrade that returned true.
	// For each IP in this channel, ShouldUpgrade(ipStr)
	// returned true within the last [UpgradeCooldown].
	recentIPsAndTimes chan ipAndTime
}

// Returns whether we should upgrade an inbound connection from [ipStr].
func (n *inboundConnThrottler) ShouldUpgrade(ipStr string) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	_, recentlyConnected := n.recentIPs[ipStr]
	if recentlyConnected {
		// We recently upgraded an inbound connection from this IP
		return false
	}

	select {
	case n.recentIPsAndTimes <- ipAndTime{
		ip:                ipStr,
		cooldownElapsedAt: n.clock.Time().Add(n.UpgradeCooldown),
	}:
		n.recentIPs[ipStr] = struct{}{}
		return true
	default:

		return false
	}
}

func (n *inboundConnThrottler) Dispatch() {
	for {
		select {
		case <-n.done:
			return
		case next := <-n.recentIPsAndTimes:
			// Sleep until it's time to remove the next IP
			time.Sleep(next.cooldownElapsedAt.Sub(n.clock.Time()))
			// Remove the next IP (we'd upgrade another inbound connection from it)
			n.lock.Lock()
			delete(n.recentIPs, next.ip)
			n.lock.Unlock()
		}
	}
}

func (n *inboundConnThrottler) Stop() {
	close(n.done)
}
