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
	// Mark that the remote address [ipStr] made an incoming connection.
	// Returns the number of times in the [reset duration]
	// that we've received an incoming connection from this address,
	// including this attempt.
	Tick(ipStr string) int
}

// Return a new connection counter
// If [resetDuration] is zero, returns a ConnMeter that always returns 0
func NewConnMeter(resetDuration time.Duration, size int) ConnMeter {
	if resetDuration == 0 {
		return &noConnMeter{}
	}
	return &connMeter{
		cache:         &cache.LRU{Size: size},
		resetDuration: resetDuration,
	}
}

type noConnMeter struct{}

func (n *noConnMeter) Tick(ipStr string) int {
	return 0
}

// connMeter implements ConnMeter
type connMeter struct {
	lock          sync.RWMutex
	cache         *cache.LRU
	resetDuration time.Duration
}

// Mark that the remote address [ipStr] made an incoming connection.
// Returns the number of times in the last [n.resetDuration]
// that we've received an incoming connection from this address,
// including this attempt.
func (n *connMeter) Tick(ipStr string) int {
	meter := n.getMeter(ipStr)
	meter.Tick()
	return meter.Ticks()
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
