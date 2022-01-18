// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// HealthCheck implements the common.VM interface
func (vm *VM) HealthCheck() (interface{}, error) {
	// Returns nil if this node is connected to > alpha percent of the Primary Network's stake
	percentConnected, err := vm.getPercentConnected()
	if err != nil {
		return nil, fmt.Errorf("couldn't get percent connected: %w", err)
	}

	vm.metrics.percentConnected.Set(percentConnected)

	details := map[string]float64{
		"percentConnected": percentConnected,
	}
	if percentConnected < constants.MinConnectedStake { // Use alpha from consensus instead of const
		return details, fmt.Errorf("connected to %f%% of the stake; should be connected to at least %f%%",
			percentConnected*100,
			constants.MinConnectedStake*100,
		)
	}
	return details, nil
}
