// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"time"
)

// Clock acts as a thin wrapper around global time that allows for easy testing
type Clock struct {
	faked bool
	time  time.Time
}

// Set the time on the clock
func (c *Clock) Set(time time.Time) { c.faked = true; c.time = time }

// Sync this clock with global time
func (c *Clock) Sync() { c.faked = false }

// Time returns the time on this clock
func (c *Clock) Time() time.Time {
	if c.faked {
		return c.time
	}
	return time.Now()
}

// Unix returns the unix time on this clock.
func (c *Clock) Unix() uint64 {
	unix := c.Time().Unix()
	if unix < 0 {
		unix = 0
	}
	return uint64(unix)
}
