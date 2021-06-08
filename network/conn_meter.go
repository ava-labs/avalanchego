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
// if the number of incoming connections from that address <= [maxConns]
// in the last [resetDuration].
// If [resetDuration] or [maxConns] is 0, returns a ConnMeter that
// allows all incoming connections.
func NewConnMeter(resetDuration time.Duration, size, maxConns int) ConnMeter {
	if resetDuration == 0 || maxConns == 0 {
		return &noConnMeter{}
	}
	return &connMeter{
		cache:         &cache.LRU{Size: size},
		resetDuration: resetDuration,
		maxConns:      maxConns,
	}
}

type noConnMeter struct{}

func (n *noConnMeter) Allow(ipStr string) bool {
	return true
}

// connMeter implements ConnMeter
type connMeter struct {
	lock          sync.RWMutex
	cache         *cache.LRU
	resetDuration time.Duration
	maxConns      int
}

// Returns whether we should allow an incoming connection from [ipStr]
func (n *connMeter) Allow(ipStr string) bool {
	meter := n.getMeter(ipStr)
	meter.Tick()
	return meter.Ticks() <= n.maxConns
}

func (n *connMeter) getMeter(ipStr string) *timer.TimedMeter {
	n.lock.Lock()
	defer n.lock.Unlock()

	if meterIntf, exists := n.cache.Get(ipStr); exists {
		return meterIntf.(*timer.TimedMeter)
	}

	// create the meter if one meter doesn't exist
	meter := &timer.TimedMeter{Duration: n.resetDuration}
	n.cache.Put(ipStr, meter)
	return meter
}
