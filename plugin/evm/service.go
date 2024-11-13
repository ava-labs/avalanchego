// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
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
	ValidationID ids.ID        `json:"validationID"`
	NodeID       ids.NodeID    `json:"nodeID"`
	Weight       uint64        `json:"weight"`
	StartTime    time.Time     `json:"startTime"`
	IsActive     bool          `json:"isActive"`
	IsSoV        bool          `json:"isSoV"`
	IsConnected  bool          `json:"isConnected"`
	Uptime       time.Duration `json:"uptime"`
}

func (api *ValidatorsAPI) GetCurrentValidators(_ *http.Request, _ *struct{}, reply *GetCurrentValidatorsResponse) error {
	api.vm.ctx.Lock.RLock()
	defer api.vm.ctx.Lock.RUnlock()

	vIDs := api.vm.validatorState.GetValidationIDs()

	reply.Validators = make([]CurrentValidator, 0, vIDs.Len())

	for _, vID := range vIDs.List() {
		validator, err := api.vm.validatorState.GetValidator(vID)
		if err != nil {
			return err
		}

		isConnected := api.vm.uptimeManager.IsConnected(validator.NodeID)

		uptime, _, err := api.vm.uptimeManager.CalculateUptime(validator.NodeID)
		if err != nil {
			return err
		}

		reply.Validators = append(reply.Validators, CurrentValidator{
			ValidationID: validator.ValidationID,
			NodeID:       validator.NodeID,
			StartTime:    validator.StartTime(),
			Weight:       validator.Weight,
			IsActive:     validator.IsActive,
			IsSoV:        validator.IsSoV,
			IsConnected:  isConnected,
			Uptime:       time.Duration(uptime.Seconds()),
		})
	}
	return nil
}
