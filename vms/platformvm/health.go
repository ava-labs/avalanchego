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

		if err := vm.updateLocalL1ValidatorBalance(subnetID); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// updateLocalL1ValidatorBalance reports the remaining balance of this node's
// L1 validator of subnetID. Nothing is reported if this node is not an L1
// validator of subnetID.
func (vm *VM) updateLocalL1ValidatorBalance(subnetID ids.ID) error {
	isL1Validator, err := vm.state.HasL1Validator(subnetID, vm.ctx.NodeID)
	if err != nil {
		return fmt.Errorf("checking for L1 validator of %s: %w", subnetID, err)
	}
	if !isL1Validator {
		vm.metrics.DeleteLocalL1ValidatorBalance(subnetID)
		return nil
	}

	// Inactive L1 validators are not in the validator manager under their
	// nodeID.
	vdr, ok := vm.Validators.GetValidator(subnetID, vm.ctx.NodeID)
	if !ok {
		vm.metrics.SetLocalL1ValidatorBalance(subnetID, 0)
		return nil
	}

	// The TxID of an active L1 validator is its validationID.
	l1Validator, err := vm.state.GetL1Validator(vdr.TxID)
	if err != nil {
		return fmt.Errorf("getting L1 validator %s: %w", vdr.TxID, err)
	}
	vm.metrics.SetLocalL1ValidatorBalance(subnetID, l1Validator.EndAccumulatedFee-vm.state.GetAccruedFees())
	return nil
}
