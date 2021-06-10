package network

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// ConnMeter keeps track of how many times a peer from a given address
// have attempted to connect to us in a given time period.
type ConnMeter interface {
	// Returns whether we should allow an incoming connection from [ipStr]
	Allow(ipStr string) bool
}

// Return a new connection meter that allows an incoming connection
// if we've allowed <= [maxConns] incoming connections from that address
// in the last [resetDuration]. Keeps the counters in a cache of
// size [connCacheSize].
// If any argument is 0, returns a ConnMeter that allows all
// incoming connections.
func NewConnMeter(resetDuration time.Duration, connCacheSize, maxConns int) ConnMeter {
	if resetDuration == 0 || maxConns == 0 || connCacheSize == 0 {
		return &noConnMeter{}
	}
	return &connMeter{
		cache:         &cache.LRU{Size: connCacheSize},
		resetDuration: resetDuration,
		maxConns:      maxConns,
		Clock:         &timer.Clock{},
	}
}

type noConnMeter struct{}

func (n *noConnMeter) Allow(string) bool { return true }

// connMeter implements ConnMeter
type connMeter struct {
	lock          sync.Mutex
	cache         *cache.LRU
	resetDuration time.Duration
	maxConns      int
	Clock         *timer.Clock
}

// Returns whether we should allow an incoming connection from [ipStr].
func (n *connMeter) Allow(ipStr string) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	meter := n.getMeter(ipStr)
	if meter.Ticks()+1 <= n.maxConns {
		meter.Tick()
		return true
	}
	return false
}

// Assumes [n.lock] is held
func (n *connMeter) getMeter(ipStr string) *timer.TimedMeter {
	if meterIntf, exists := n.cache.Get(ipStr); exists {
		return meterIntf.(*timer.TimedMeter)
	}

	// create the meter if one meter doesn't exist
	meter := &timer.TimedMeter{
		Duration: n.resetDuration,
		Clock:    n.Clock,
	}
	n.cache.Put(ipStr, meter)
	return meter
}
