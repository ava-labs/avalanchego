// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer"
)

// Maximum number of recent incoming connections allowed.
// "recent" is defined by the [allowCooldown] used by the incomingConnThrottler.
// In practice, we should never have this many recent incoming connections.
const maxRecentIncomingConnections = 1024

var (
	_ IncomingConnThrottler = &incomingConnThrottler{}
	_ IncomingConnThrottler = &noIncomingConnThrottler{}
)

// IncomingConnThrottler decides whether to allow an incoming connection from IP [ipStr].
// If Allow returns false, the connection to this IP should be closed.
type IncomingConnThrottler interface {
	// Dispatch starts this IncomingConnThrottler.
	// Must be called before [Allow].
	// Blocks until [Stop] is called (i.e. should be called in a goroutine.)
	Dispatch()
	// Stop this IncomingConnThrottler and causes [Dispatch] to return if it has been called.
	// This IncomingConnThrottler must not be used after [Stop] is called.
	Stop()
	// Returns whether we should allow an incoming connection from [ipStr].
	// Must only be called after [Dispatch] has been called.
	// Must not be called after [Stop] has been called.
	Allow(ipStr string) bool
}

// Returns an IncomingConnThrottler that allows an incoming connection from a given IP
// every [allowCooldown]. If [allowCooldown] == 0, allows all incoming connections.
func NewIncomingConnThrottler(allowCooldown time.Duration) IncomingConnThrottler {
	if allowCooldown == 0 {
		return &noIncomingConnThrottler{}
	}
	return &incomingConnThrottler{
		done:              make(chan struct{}, 1),
		allowCooldown:     allowCooldown,
		recentIPs:         make(map[string]struct{}),
		recentIPsAndTimes: make(chan ipAndTime, maxRecentIncomingConnections),
	}
}

// noIncomingConnThrottler allows all incoming connections
type noIncomingConnThrottler struct{}

func (*noIncomingConnThrottler) Dispatch()         {}
func (*noIncomingConnThrottler) Stop()             {}
func (*noIncomingConnThrottler) Allow(string) bool { return true }

type ipAndTime struct {
	ip                string
	cooldownElapsedAt time.Time
}

// incomingConnThrottler implements IncomingConnThrottler
type incomingConnThrottler struct {
	lock sync.Mutex
	// Useful for faking time in tests
	clock timer.Clock
	// When [done] is closed, Dispatch returns.
	done chan struct{}
	// Allow(IP) returns true if it has been at least [allowCooldown]
	// since the last time Allow(IP) returned true or if
	// Allow(IP) has never been called.
	allowCooldown time.Duration
	// IP --> Present if Allow(IP) returned true
	// within the last [allowCooldown].
	recentIPs map[string]struct{}
	// Sorted in order of increasing time
	// of last call to Allow that returned true.
	// For each IP in this channel, Allow(IP)
	// returned true within the last [allowCooldown].
	recentIPsAndTimes chan ipAndTime
}

// Returns whether we should allow an incoming connection from [ipStr].
func (n *incomingConnThrottler) Allow(ipStr string) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	_, recentlyConnected := n.recentIPs[ipStr]
	if recentlyConnected {
		// We recently allowed an incoming connection from this IP
		return false
	}

	select {
	case n.recentIPsAndTimes <- ipAndTime{
		ip:                ipStr,
		cooldownElapsedAt: n.clock.Time().Add(n.allowCooldown),
	}:
		n.recentIPs[ipStr] = struct{}{}
		return true
	default:
		// Too many incoming connections recently.
		// This should never happen in practice.
		return false
	}
}

func (n *incomingConnThrottler) Dispatch() {
	for {
		select {
		case <-n.done:
			return
		case next := <-n.recentIPsAndTimes:
			// Sleep until it's time to remove the next IP
			time.Sleep(next.cooldownElapsedAt.Sub(n.clock.Time()))
			// Remove the next IP (we'll allow another incoming connection from it)
			n.lock.Lock()
			delete(n.recentIPs, next.ip)
			n.lock.Unlock()
		}
	}
}

func (n *incomingConnThrottler) Stop() {
	close(n.done)
}
