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

type ClientControl interface {
	// Register an IP returns the current ticks
	RegisterConnection(addr string) (int, error)
	// Unregister the IP, removing the meter
	UnRegisterConnection(addr string)
}

func NewClientControl(timeout *time.Duration, size int) ClientControl {
	if timeout == nil {
		return &noClientControl{}
	}
	return &clientControl{cache: &cache.LRU{Size: size}}
}

type noClientControl struct {
}

func (n *noClientControl) RegisterConnection(addr string) (int, error) {
	return 0, nil
}

func (n *noClientControl) UnRegisterConnection(addr string) {
}

type clientControl struct {
	lock    sync.RWMutex
	cache   *cache.LRU
	timeout time.Duration
}

func (n *clientControl) UnRegisterConnection(addr string) {
	ip, err := utils.ToIPDesc(addr)
	if err != nil {
		return
	}
	addr = ip.IP.String()
	n.unRegisterConnectionAddress(addr)
}

func (n *clientControl) RegisterConnection(addr string) (int, error) {
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

func (n *clientControl) unRegisterConnectionAddress(addr string) {
	id, err := ids.ToID(hashing.ComputeHash256([]byte(addr)))
	if err != nil {
		return
	}

	n.lock.Lock()
	defer n.lock.Unlock()
	n.cache.Evict(id)
}

func (n *clientControl) registerConnectionAddress(addr string) (*timer.TimedMeter, error) {
	id, err := ids.ToID(hashing.ComputeHash256([]byte(addr)))
	if err != nil {
		return nil, err
	}

	// lets get the meter
	n.lock.RLock()
	meter, exists := n.cache.Get(id)
	n.lock.RUnlock()

	// if the meter doesn't exist we'll create one
	if !exists {
		n.lock.Lock()

		// lets just confirm under lock it wasn't create.
		// some other thread could of added the IP.
		meter, exists = n.cache.Get(id)
		if !exists {
			meter = &timer.TimedMeter{Duration: n.timeout}
			n.cache.Put(id, meter)
		}
		n.lock.Unlock()
	}
	return meter.(*timer.TimedMeter), nil
}
