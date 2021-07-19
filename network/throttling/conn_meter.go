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

// InboundConnThrottler decides whether to allow an inbound connection from IP [ipStr].
// If Allow(ipStr) returns false, the connection to that IP should be closed.
type InboundConnThrottler interface {
	// Dispatch starts this InboundConnThrottler.
	// Must be called before [Allow].
	// Blocks until [Stop] is called (i.e. should be called in a goroutine.)
	Dispatch()
	// Stop this InboundConnThrottler and causes [Dispatch] to return.
	// Should be called when we're done with this InboundConnThrottler.
	// This InboundConnThrottler must not be used after [Stop] is called.
	Stop()
	// Returns whether we should allow an inbound connection from [ipStr].
	// Must only be called after [Dispatch] has been called.
	// Must not be called after [Stop] has been called.
	Allow(ipStr string) bool
}

type InboundConnThrottlerConfig struct {
	// Allow(ipStr) returns true if it has been at least [AllowCooldown]
	// since the last time Allow(ipStr) returned true or if
	// Allow(ipStr) has never been called.
	// If <= 0, inbound connections not rate-limited.
	AllowCooldown time.Duration
	// Maximum number of inbound connections allowed within [AllowCooldown].
	// (As implemented in inboundConnThrottler, may actually allow
	// [MaxRecentConns+1] due to a race condition but that's fine.)
	// If <= 0, inbound connections not rate-limited.
	MaxRecentConns int
}

// Returns an InboundConnThrottler that allows an inbound connection from a given IP
// every [AllowCooldown].
func NewInboundConnThrottler(log logging.Logger, config InboundConnThrottlerConfig) InboundConnThrottler {
	if config.AllowCooldown <= 0 || config.MaxRecentConns <= 0 {
		return &noInboundConnThrottler{}
	}
	return &inboundConnThrottler{
		InboundConnThrottlerConfig: config,
		log:                        log,
		done:                       make(chan struct{}, 1),
		recentIPs:                  make(map[string]struct{}),
		recentIPsAndTimes:          make(chan ipAndTime, config.MaxRecentConns),
	}
}

// noInboundConnThrottler allows all inbound connections
type noInboundConnThrottler struct{}

func (*noInboundConnThrottler) Dispatch()         {}
func (*noInboundConnThrottler) Stop()             {}
func (*noInboundConnThrottler) Allow(string) bool { return true }

type ipAndTime struct {
	ip                string
	cooldownElapsedAt time.Time
}

// inboundConnThrottler implements InboundConnThrottler
type inboundConnThrottler struct {
	InboundConnThrottlerConfig
	log  logging.Logger
	lock sync.Mutex
	// Useful for faking time in tests
	clock timer.Clock
	// When [done] is closed, Dispatch returns.
	done chan struct{}
	// IP --> Present if Allow(ipStr) returned true
	// within the last [AllowCooldown].
	recentIPs map[string]struct{}
	// Sorted in order of increasing time
	// of last call to Allow that returned true.
	// For each IP in this channel, Allow(ipStr)
	// returned true within the last [AllowCooldown].
	recentIPsAndTimes chan ipAndTime
}

// Returns whether we should allow an inbound connection from [ipStr].
func (n *inboundConnThrottler) Allow(ipStr string) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	_, recentlyConnected := n.recentIPs[ipStr]
	if recentlyConnected {
		// We recently allowed an inbound connection from this IP
		return false
	}

	select {
	case n.recentIPsAndTimes <- ipAndTime{
		ip:                ipStr,
		cooldownElapsedAt: n.clock.Time().Add(n.AllowCooldown),
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
			// Remove the next IP (we'll allow another inbound connection from it)
			n.lock.Lock()
			delete(n.recentIPs, next.ip)
			n.lock.Unlock()
		}
	}
}

func (n *inboundConnThrottler) Stop() {
	close(n.done)
}
