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

type GetCurrentValidatorsResponse struct {
	Validators []CurrentValidator `json:"validators"`
}

type CurrentValidator struct {
	ValidationID   ids.ID        `json:"validationID"`
	NodeID         ids.NodeID    `json:"nodeID"`
	Weight         uint64        `json:"weight"`
	StartTimestamp uint64        `json:"startTimestamp"`
	IsActive       bool          `json:"isActive"`
	IsL1Validator  bool          `json:"isL1Validator"`
	IsConnected    bool          `json:"isConnected"`
	Uptime         time.Duration `json:"uptime"`
}

func (api *ValidatorsAPI) GetCurrentValidators(_ *http.Request, _ *struct{}, reply *GetCurrentValidatorsResponse) error {
	api.vm.ctx.Lock.RLock()
	defer api.vm.ctx.Lock.RUnlock()

	vIDs := api.vm.validatorsManager.GetValidationIDs()

	reply.Validators = make([]CurrentValidator, 0, vIDs.Len())

	for _, vID := range vIDs.List() {
		validator, err := api.vm.validatorsManager.GetValidator(vID)
		if err != nil {
			return err
		}

		isConnected := api.vm.validatorsManager.IsConnected(validator.NodeID)

		uptime, _, err := api.vm.validatorsManager.CalculateUptime(validator.NodeID)
		if err != nil {
			return err
		}

		reply.Validators = append(reply.Validators, CurrentValidator{
			ValidationID:   validator.ValidationID,
			NodeID:         validator.NodeID,
			StartTimestamp: validator.StartTimestamp,
			Weight:         validator.Weight,
			IsActive:       validator.IsActive,
			IsL1Validator:  validator.IsL1Validator,
			IsConnected:    isConnected,
			Uptime:         time.Duration(uptime.Seconds()),
		})
	}
	return nil
}
