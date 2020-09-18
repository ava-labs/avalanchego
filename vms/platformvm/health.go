// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/AppsFlyer/go-sundheit/checks"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// HealthChecks implements the common.VM interface
// It returns a list of health checks to periodically perform
// on this chain.
// These checks assume the VM lock is held while they execute.
func (vm *VM) HealthChecks() []checks.Check {
	// Returns nil iff this node is connected to > alpha percent of the Primary Network's stake
	isWellConnected := func() (interface{}, error) {
		percentConnected, err := vm.getPercentConnected()
		if err != nil {
			return nil, fmt.Errorf("couldn't get percent connected: %w", err)
		}
		details := map[string]float64{
			"percentConnected": percentConnected,
		}
		if percentConnected < constants.MinConnectedStake { // Use alpha from consensus instead of const
			return details, fmt.Errorf("connected to %f percent of the stake; should be connected to at least %f",
				percentConnected,
				constants.MinConnectedStake,
			)
		}
		return details, nil
	}
	check := health.NewCheck("isWellConnected", isWellConnected)
	return []checks.Check{check}
}
