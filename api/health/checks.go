// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"errors"
	"time"
)

var (
	// ErrHeartbeatNotDetected is returned from a HeartbeatCheckFn when the
	// heartbeat has not been detected recently enough
	ErrHeartbeatNotDetected = errors.New("heartbeat not detected")
)

// CheckFn returns optional status information and an error indicating health or
// non-health
type CheckFn func() (interface{}, error)

// Check defines a single health check that we want to monitor and consider as
// part of our wider healthiness
type Check struct {
	// Name is the identifier for this check and must be unique among all Checks
	Name string

	// CheckFn is the function to call to perform the the health check
	CheckFn CheckFn

	// ExecutionPeriod is the duration to wait between executions of this Check
	ExecutionPeriod time.Duration

	// InitialDelay is the duration to wait before executing the first time
	InitialDelay time.Duration

	// InitiallyPassing is whether or not to consider the Check healthy before the
	// initial execution
	InitiallyPassing bool
}

// gosundheitCheck implements the health.Check interface backed by a CheckFn
type gosundheitCheck struct {
	name    string
	checkFn CheckFn
}

// Name implements the health.Check interface by returning a unique name
func (c gosundheitCheck) Name() string { return c.name }

// Execute implements the health.Check interface by executing the checkFn and
// returning the results
func (c gosundheitCheck) Execute() (interface{}, error) { return c.checkFn() }

// Heartbeater provides a getter to the most recently observed heartbeat
type Heartbeater interface {
	GetHeartbeat() int64
}

// HeartbeatCheckFn returns a CheckFn that checks the given heartbeater has
// pulsed within the given duration
func HeartbeatCheckFn(hb Heartbeater, max time.Duration) CheckFn {
	return func() (data interface{}, err error) {
		// Get the heartbeat and create a data set to return to the caller
		hb := hb.GetHeartbeat()
		data = map[string]int64{"heartbeat": hb}

		// If the current time is after the last known heartbeat + the limit then
		// mark our check as failed
		if time.Unix(hb, 0).Add(max).Before(time.Now()) {
			err = ErrHeartbeatNotDetected
		}
		return data, err
	}
}
