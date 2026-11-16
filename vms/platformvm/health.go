// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	for subnetID := range vm.TrackedSubnets {
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

		balance, err := vm.localL1ValidatorBalance(subnetID)
		if err != nil {
			return nil, err
		}
		vm.metrics.SetLocalL1ValidatorBalance(subnetID, balance)
	}
	return nil, nil
}

// localL1ValidatorBalance returns the remaining balance of this node's L1
// validator of subnetID, or 0 if this node is not an active L1 validator.
func (vm *VM) localL1ValidatorBalance(subnetID ids.ID) (uint64, error) {
	vdr, ok := vm.Validators.GetValidator(subnetID, vm.ctx.NodeID)
	if !ok {
		return 0, nil
	}

	// The TxID of an active L1 validator is its validationID.
	l1Validator, err := vm.state.GetL1Validator(vdr.TxID)
	switch err {
	case nil:
		return l1Validator.EndAccumulatedFee - vm.state.GetAccruedFees(), nil
	case database.ErrNotFound:
		// This node is a legacy subnet validator.
		return 0, nil
	default:
		return 0, fmt.Errorf("getting L1 validator %s: %w", vdr.TxID, err)
	}
}
