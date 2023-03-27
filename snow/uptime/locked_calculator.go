// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

var (
	errStillBootstrapping = errors.New("still bootstrapping")

	_ LockedCalculator = (*lockedCalculator)(nil)
)

type LockedCalculator interface {
	Calculator

	SetCalculator(vmState *utils.Atomic[snow.State], lock sync.Locker, newC Calculator)
}

type lockedCalculator struct {
	lock           sync.RWMutex
	vmState        *utils.Atomic[snow.State]
	calculatorLock sync.Locker
	c              Calculator
}

func NewLockedCalculator() LockedCalculator {
	return &lockedCalculator{}
}

func (c *lockedCalculator) CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Time, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.vmState == nil || !status.DoneBootstraping(c.vmState.Get()) {
		return 0, time.Time{}, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptime(nodeID, subnetID)
}

func (c *lockedCalculator) CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.vmState == nil || !status.DoneBootstraping(c.vmState.Get()) {
		return 0, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptimePercent(nodeID, subnetID)
}

func (c *lockedCalculator) CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.vmState == nil || !status.DoneBootstraping(c.vmState.Get()) {
		return 0, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptimePercentFrom(nodeID, subnetID, startTime)
}

func (c *lockedCalculator) SetCalculator(vmState *utils.Atomic[snow.State], lock sync.Locker, newC Calculator) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.vmState = vmState
	c.calculatorLock = lock
	c.c = newC
}
