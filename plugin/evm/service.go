// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

	"github.com/ava-labs/subnet-evm/plugin/evm/client"
)

type ValidatorsAPI struct {
	vm *VM
}

func (api *ValidatorsAPI) GetCurrentValidators(httpReq *http.Request, req *client.GetCurrentValidatorsRequest, reply *client.GetCurrentValidatorsResponse) error {
	api.vm.vmLock.RLock()
	defer api.vm.vmLock.RUnlock()
	ctx := httpReq.Context()

	validatorSet, _, err := api.vm.ctx.ValidatorState.GetCurrentValidatorSet(ctx, api.vm.ctx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	// Filter by requested nodeIDs if specified
	requestedNodeIDs := set.NewSet[ids.NodeID](len(req.NodeIDs))
	if len(req.NodeIDs) > 0 {
		for _, nodeID := range req.NodeIDs {
			requestedNodeIDs.Add(nodeID)
		}
	}

	reply.Validators = make([]client.CurrentValidator, 0, len(validatorSet))
	for validationID, validator := range validatorSet {
		// Skip if specific nodeIDs were requested and this isn't one of them
		if requestedNodeIDs.Len() > 0 && !requestedNodeIDs.Contains(validator.NodeID) {
			continue
		}

		upDuration, lastUpdated, found, err := api.vm.uptimeTracker.GetUptime(validationID)
		if err != nil {
			return fmt.Errorf("failed to get uptime for validation ID %s: %w", validationID, err)
		}
		if !found {
			return fmt.Errorf("validator not found for validation ID %s", validationID)
		}

		var uptimeFloat float64

		startTime := time.Unix(int64(validator.StartTime), 0)
		bestPossibleUpDuration := lastUpdated.Sub(startTime)
		if bestPossibleUpDuration == 0 {
			uptimeFloat = 1
		} else {
			uptimeFloat = float64(upDuration) / float64(bestPossibleUpDuration)
		}
		// Transform this to a percentage (0-100) to make it consistent
		// with currentValidators in PlatformVM API
		uptimePercentage := float32(uptimeFloat * 100)
		uptimeSeconds := uint64(upDuration.Seconds())

		isConnected := api.vm.P2PValidators().Has(ctx, validator.NodeID)

		reply.Validators = append(reply.Validators, client.CurrentValidator{
			ValidationID:     validationID,
			NodeID:           validator.NodeID,
			StartTimestamp:   validator.StartTime,
			Weight:           validator.Weight,
			IsActive:         validator.IsActive,
			IsL1Validator:    validator.IsL1Validator,
			IsConnected:      isConnected,
			UptimePercentage: uptimePercentage,
			UptimeSeconds:    uptimeSeconds,
		})
	}
	return nil
}
