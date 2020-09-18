package avm

import (
	"github.com/AppsFlyer/go-sundheit/checks"
)

// HealthChecks implements the common.VM interface
// It returns a list of health checks to periodically perform
// on this chain.
// TODO add health checks
func (vm *VM) HealthChecks() []checks.Check {
	return nil
}
