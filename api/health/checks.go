// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"github.com/AppsFlyer/go-sundheit/checks"
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
	name    string
	checkFn func() (interface{}, error)
}

// Name is the identifier for this check and must be unique among all Checks
func (c check) Name() string { return c.name }

// Execute performs the health check. It returns nil if the check passes.
// It can also return additional information to marshal and display to the caller
func (c check) Execute() (interface{}, error) { return c.checkFn() }

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
