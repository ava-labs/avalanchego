// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/constants"
)

const fallbackMinPercentConnected = 0.8

var errNotEnoughStake = errors.New("not connected to enough stake")

func (vm *VM) HealthCheck(context.Context) (interface{}, error) {
	// Returns nil if this node is connected to > alpha percent of the Primary Network's stake
	primaryPercentConnected, err := vm.getPercentConnected(constants.PrimaryNetworkID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get percent connected: %w", err)
	}
	vm.metrics.SetPercentConnected(primaryPercentConnected)
	details := map[string]float64{
		"primary-percentConnected": primaryPercentConnected,
	}

	localPrimaryValidator, err := vm.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		vm.ctx.NodeID,
	)
	switch err {
	case nil:
		vm.metrics.SetTimeUntilUnstake(time.Until(localPrimaryValidator.EndTime))
	case database.ErrNotFound:
		vm.metrics.SetTimeUntilUnstake(0)
	default:
		return nil, fmt.Errorf("couldn't get current local validator: %w", err)
	}

	primaryMinPercentConnected, ok := vm.MinPercentConnectedStakeHealthy[constants.PrimaryNetworkID]
	if !ok {
		// This should never happen according to the comment for
		// [MinPercentConnectedStakeHealthy] but we include it here to avoid the
		// situation where a regression causes the key to be missing so that we
		// don't accidentally set [primaryMinPercentConnected] to 0.
		vm.ctx.Log.Warn("primary network min connected stake not given",
			zap.Float64("fallback value", fallbackMinPercentConnected),
		)
		primaryMinPercentConnected = fallbackMinPercentConnected
	}

	var errorReasons []string
	if primaryPercentConnected < primaryMinPercentConnected {
		errorReasons = append(errorReasons,
			fmt.Sprintf("connected to %f%% of primary network stake; should be connected to at least %f%%",
				primaryPercentConnected*100,
				primaryMinPercentConnected*100,
			),
		)
	}

	for subnetID := range vm.TrackedSubnets {
		percentConnected, err := vm.getPercentConnected(subnetID)
		if err != nil {
			return nil, fmt.Errorf("couldn't get percent connected for %q: %w", subnetID, err)
		}
		minPercentConnected, ok := vm.MinPercentConnectedStakeHealthy[subnetID]
		if !ok {
			minPercentConnected = primaryMinPercentConnected
		}

		vm.metrics.SetSubnetPercentConnected(subnetID, percentConnected)
		key := fmt.Sprintf("%s-percentConnected", subnetID)
		details[key] = percentConnected

		localSubnetValidator, err := vm.state.GetCurrentValidator(
			subnetID,
			vm.ctx.NodeID,
		)
		switch err {
		case nil:
			vm.metrics.SetTimeUntilSubnetUnstake(subnetID, time.Until(localSubnetValidator.EndTime))
		case database.ErrNotFound:
			vm.metrics.SetTimeUntilSubnetUnstake(subnetID, 0)
		default:
			return nil, fmt.Errorf("couldn't get current subnet validator of %q: %w", subnetID, err)
		}

		if percentConnected < minPercentConnected {
			errorReasons = append(errorReasons,
				fmt.Sprintf("connected to %f%% of %q weight; should be connected to at least %f%%",
					percentConnected*100,
					subnetID,
					minPercentConnected*100,
				),
			)
		}
	}

	if len(errorReasons) == 0 || !vm.StakingEnabled {
		return details, nil
	}
	return details, fmt.Errorf("platform layer is unhealthy err: %w, details: %s",
		errNotEnoughStake,
		strings.Join(errorReasons, ", "),
	)
}
