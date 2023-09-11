// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func (vm *VM) HealthCheck(context.Context) (interface{}, error) {
	nodeID := ids.GenericNodeIDFromNodeID(vm.ctx.NodeID)
	localPrimaryValidator, err := vm.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		nodeID,
	)
	switch err {
	case nil:
		vm.metrics.SetTimeUntilUnstake(time.Until(localPrimaryValidator.EndTime))
	case database.ErrNotFound:
		vm.metrics.SetTimeUntilUnstake(0)
	default:
		return nil, fmt.Errorf("couldn't get current local validator: %w", err)
	}

	for subnetID := range vm.TrackedSubnets {
		localSubnetValidator, err := vm.state.GetCurrentValidator(
			subnetID,
			nodeID,
		)
		switch err {
		case nil:
			vm.metrics.SetTimeUntilSubnetUnstake(subnetID, time.Until(localSubnetValidator.EndTime))
		case database.ErrNotFound:
			vm.metrics.SetTimeUntilSubnetUnstake(subnetID, 0)
		default:
			return nil, fmt.Errorf("couldn't get current subnet validator of %q: %w", subnetID, err)
		}
	}
	return nil, nil
}
