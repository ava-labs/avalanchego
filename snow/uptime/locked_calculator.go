// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errNotReady = errors.New("should not be called")

	_ LockedCalculator = &lockedCalculator{}
)

type LockedCalculator interface {
	Calculator

	SetCalculator(ctx *snow.Context, newC Calculator)
}

type lockedCalculator struct {
	lock sync.RWMutex
	ctx  *snow.Context
	c    Calculator
}

func NewLockedCalculator() LockedCalculator {
	return &lockedCalculator{}
}

func (c *lockedCalculator) CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.ctx == nil || !c.ctx.IsBootstrapped() {
		return 0, time.Time{}, errNotReady
	}

	c.ctx.Lock.Lock()
	defer c.ctx.Lock.Unlock()

	return c.c.CalculateUptime(nodeID)
}

func (c *lockedCalculator) CalculateUptimePercent(nodeID ids.ShortID) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.ctx == nil || !c.ctx.IsBootstrapped() {
		return 0, errNotReady
	}

	c.ctx.Lock.Lock()
	defer c.ctx.Lock.Unlock()

	return c.c.CalculateUptimePercent(nodeID)
}

func (c *lockedCalculator) CalculateUptimePercentFrom(nodeID ids.ShortID, startTime time.Time) (float64, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.ctx == nil || !c.ctx.IsBootstrapped() {
		return 0, errNotReady
	}

	c.ctx.Lock.Lock()
	defer c.ctx.Lock.Unlock()

	return c.c.CalculateUptimePercentFrom(nodeID, startTime)
}

func (c *lockedCalculator) SetCalculator(ctx *snow.Context, newC Calculator) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.ctx = ctx
	c.c = newC
}
