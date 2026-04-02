// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	errStillBootstrapping = errors.New("still bootstrapping")

	_ LockedCalculator = (*lockedCalculator)(nil)
)

type LockedCalculator interface {
	Calculator

	SetCalculator(isBootstrapped *utils.Atomic[bool], lock sync.Locker, newC Calculator)
}

type lockedCalculator struct {
	lock           sync.RWMutex
	isBootstrapped *utils.Atomic[bool]
	calculatorLock sync.Locker
	c              Calculator
}

func NewLockedCalculator() LockedCalculator {
	return &lockedCalculator{}
}

func (c *lockedCalculator) CalculateUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.isBootstrapped == nil || !c.isBootstrapped.Get() {
		return 0, time.Time{}, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptime(nodeID)
}

func (c *lockedCalculator) CalculateUptimePercent(nodeID ids.NodeID) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.isBootstrapped == nil || !c.isBootstrapped.Get() {
		return 0, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptimePercent(nodeID)
}

func (c *lockedCalculator) CalculateUptimePercentFrom(nodeID ids.NodeID, startTime time.Time) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.isBootstrapped == nil || !c.isBootstrapped.Get() {
		return 0, errStillBootstrapping
	}

	c.calculatorLock.Lock()
	defer c.calculatorLock.Unlock()

	return c.c.CalculateUptimePercentFrom(nodeID, startTime)
}

func (c *lockedCalculator) SetCalculator(isBootstrapped *utils.Atomic[bool], lock sync.Locker, newC Calculator) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.isBootstrapped = isBootstrapped
	c.calculatorLock = lock
	c.c = newC
}
