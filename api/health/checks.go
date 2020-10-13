// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"errors"
	"time"

	"github.com/AppsFlyer/go-sundheit/checks"
)

var (
	// ErrHeartbeatNotDetected is returned from a HeartbeatCheckFn when the
	// heartbeat has not been detected recently enough
	ErrHeartbeatNotDetected = errors.New("heartbeat not detected")
)

// NewCheck creates a new check with name [name] that calls [execute]
// to evalute health.
func NewCheck(name string, execute func() (interface{}, error)) checks.Check {
	return &check{
		name:    name,
		checkFn: execute,
	}
}

// check implements the Check interface
type check struct {
	name                          string
	checkFn                       func() (interface{}, error)
	executionPeriod, initialDelay time.Duration
	initiallyPassing              bool
}

// Name is the identifier for this check and must be unique among all Checks
func (c check) Name() string { return c.name }

// Execute performs the health check. It returns nil if the check passes.
// It can also return additional information to marshal and display to the caller
func (c check) Execute() (interface{}, error) { return c.checkFn() }

// ExecutionPeriod is the duration to wait between executions of this Check
func (c check) ExecutionPeriod() time.Duration { return c.executionPeriod }

// InitialDelay is the duration to wait before executing the first time
func (c check) InitialDelay() time.Duration { return c.initialDelay }

// InitiallyPassing is whether or not to consider the Check healthy before the initial execution
func (c check) InitiallyPassing() bool { return c.initiallyPassing }

// monotonicCheck is a check that will run until it passes once, and after that it will
// always pass without performing any logic. Used for bootstrapping, for example.
type monotonicCheck struct {
	passed bool
	check
}

func (mc monotonicCheck) Execute() (interface{}, error) {
	if mc.passed {
		return nil, nil
	}
	details, pass := mc.check.Execute()
	if pass == nil {
		mc.passed = true
	}
	return details, pass
}

// Heartbeater provides a getter to the most recently observed heartbeat
type Heartbeater interface {
	GetHeartbeat() int64
}

// HeartbeatCheckFn returns a CheckFn that checks the given heartbeater has
// pulsed within the given duration
func HeartbeatCheckFn(hb Heartbeater, max time.Duration) func() (interface{}, error) {
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
