// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type ValidatorsAPI struct {
	vm *VM
}

type GetCurrentValidatorsRequest struct {
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

type GetCurrentValidatorsResponse struct {
	Validators []CurrentValidator `json:"validators"`
}

type CurrentValidator struct {
	ValidationID     ids.ID     `json:"validationID"`
	NodeID           ids.NodeID `json:"nodeID"`
	Weight           uint64     `json:"weight"`
	StartTimestamp   uint64     `json:"startTimestamp"`
	IsActive         bool       `json:"isActive"`
	IsL1Validator    bool       `json:"isL1Validator"`
	IsConnected      bool       `json:"isConnected"`
	UptimePercentage float32    `json:"uptimePercentage"`
	UptimeSeconds    uint64     `json:"uptimeSeconds"`
}

func (api *ValidatorsAPI) GetCurrentValidators(_ *http.Request, req *GetCurrentValidatorsRequest, reply *GetCurrentValidatorsResponse) error {
	api.vm.ctx.Lock.RLock()
	defer api.vm.ctx.Lock.RUnlock()

	var vIDs set.Set[ids.ID]
	if len(req.NodeIDs) > 0 {
		vIDs = set.NewSet[ids.ID](len(req.NodeIDs))
		for _, nodeID := range req.NodeIDs {
			vID, err := api.vm.validatorsManager.GetValidationID(nodeID)
			if err != nil {
				return fmt.Errorf("couldn't find validator with node ID %s", nodeID)
			}
			vIDs.Add(vID)
		}
	} else {
		vIDs = api.vm.validatorsManager.GetValidationIDs()
	}

	reply.Validators = make([]CurrentValidator, 0, vIDs.Len())

	for _, vID := range vIDs.List() {
		validator, err := api.vm.validatorsManager.GetValidator(vID)
		if err != nil {
			return fmt.Errorf("couldn't find validator with validation ID %s", vID)
		}

		isConnected := api.vm.validatorsManager.IsConnected(validator.NodeID)

		upDuration, lastUpdated, err := api.vm.validatorsManager.CalculateUptime(validator.NodeID)
		if err != nil {
			return err
		}
		var uptimeFloat float64
		startTime := time.Unix(int64(validator.StartTimestamp), 0)
		bestPossibleUpDuration := lastUpdated.Sub(startTime)
		if bestPossibleUpDuration == 0 {
			uptimeFloat = 1
		} else {
			uptimeFloat = float64(upDuration) / float64(bestPossibleUpDuration)
		}

		// Transform this to a percentage (0-100) to make it consistent
		// with currentValidators in PlatformVM API
		uptimePercentage := float32(uptimeFloat * 100)

		reply.Validators = append(reply.Validators, CurrentValidator{
			ValidationID:     validator.ValidationID,
			NodeID:           validator.NodeID,
			StartTimestamp:   validator.StartTimestamp,
			Weight:           validator.Weight,
			IsActive:         validator.IsActive,
			IsL1Validator:    validator.IsL1Validator,
			IsConnected:      isConnected,
			UptimePercentage: uptimePercentage,
			UptimeSeconds:    uint64(upDuration.Seconds()),
		})
	}
	return nil
}
