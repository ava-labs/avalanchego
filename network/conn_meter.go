package network

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// ConnMeter keeps track of how many times a peer from a given address
// have attempted to connect to us in a given time period.
type ConnMeter interface {
	// Register that the given address tried to connect to us.
	// Returns the number of times they previously tried.
	Register(addr string) (int, error)
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

func (n *noConnMeter) Register(addr string) (int, error) {
	return 0, nil
}

// connMeter implements ConnMeter
type connMeter struct {
	lock          sync.RWMutex
	cache         *cache.LRU
	resetDuration time.Duration
}

func (n *connMeter) Register(addr string) (int, error) {
	ip, err := utils.ToIPDesc(addr)
	if err != nil {
		return 0, err
	}

	// normalize to just the incoming IP
	addr = ip.IP.String()
	meter, err := n.registerAddress(addr)
	if err != nil {
		return 0, err
	}

	tickCount := meter.Ticks()
	meter.Tick()
	return tickCount, nil
}

func (n *connMeter) registerAddress(addr string) (*timer.TimedMeter, error) {
	id, err := ids.ToID(hashing.ComputeHash256([]byte(addr)))
	if err != nil {
		return nil, err
	}

	// Get the meter
	meter, exists := n.cache.Get(id)
	if exists {
		return meter.(*timer.TimedMeter), nil
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	meter, exists = n.cache.Get(id)
	if exists {
		return meter.(*timer.TimedMeter), nil
	}

	// create the meter if one meter doesn't exist
	meter = &timer.TimedMeter{Duration: n.resetDuration}
	n.cache.Put(id, meter)
	return meter.(*timer.TimedMeter), nil
}
