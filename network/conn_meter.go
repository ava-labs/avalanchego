package network

import (
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
	RegisterConnection(addr string) (int, error)
	// Unregister the IP, removing the meter
	UnRegisterConnection(addr string)
}

// Return a new connection counter
// If [resetDuration] is zero, returns a ConnMeter that always returns 0
func NewConnMeter(resetDuration time.Duration, size int) ConnMeter {
	if resetDuration == 0 {
		return &noConnMeter{}
	}
	return &connMeter{cache: &cache.LRU{Size: size}}
}

type noConnMeter struct{}

func (n *noConnMeter) RegisterConnection(addr string) (int, error) {
	return 0, nil
}

func (n *noConnMeter) UnRegisterConnection(addr string) {}

// connMeter implements ConnMeter
type connMeter struct {
	cache         *cache.LRU
	resetDuration time.Duration
}

func (n *connMeter) UnRegisterConnection(addr string) {
	ip, err := utils.ToIPDesc(addr)
	if err != nil {
		return
	}
	addr = ip.IP.String()
	n.unRegisterConnectionAddress(addr)
}

func (n *connMeter) RegisterConnection(addr string) (int, error) {
	ip, err := utils.ToIPDesc(addr)
	if err != nil {
		return 0, err
	}

	// normalize to just the incoming IP
	addr = ip.IP.String()
	meter, err := n.registerConnectionAddress(addr)
	if err != nil {
		return 0, err
	}

	tickCount := meter.Ticks()
	meter.Tick()
	return tickCount, nil
}

func (n *connMeter) unRegisterConnectionAddress(addr string) {
	id, err := ids.ToID(hashing.ComputeHash256([]byte(addr)))
	if err != nil {
		return
	}

	n.cache.Evict(id)
}

func (n *connMeter) registerConnectionAddress(addr string) (*timer.TimedMeter, error) {
	id, err := ids.ToID(hashing.ComputeHash256([]byte(addr)))
	if err != nil {
		return nil, err
	}

	// Get the meter
	meter, exists := n.cache.Get(id)

	// create the meter if one meter doesn't exist
	if !exists {
		meter = &timer.TimedMeter{Duration: n.resetDuration}
		n.cache.Put(id, meter)
	}
	return meter.(*timer.TimedMeter), nil
}
