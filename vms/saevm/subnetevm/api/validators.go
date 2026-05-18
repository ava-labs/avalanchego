// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/set"

	avagovalidators "github.com/ava-labs/avalanchego/snow/validators"
)

// UptimeSource is the slice of `*uptimetracker.UptimeTracker` consumed
// by `ValidatorsAPI`. Pulled out as a one-method interface so call
// sites do not bind to the concrete tracker type and so tests can
// inject stubs without constructing a full tracker. The exported
// signature matches `*uptimetracker.UptimeTracker.GetUptime` exactly:
// the concrete tracker satisfies it without an adapter.
type UptimeSource interface {
	GetUptime(validationID ids.ID) (time.Duration, time.Time, error)
}

// CurrentValidator is one entry in the response of
// `validators.getCurrentValidators`.
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

// GetCurrentValidatorsRequest filters the response to a specific set of
// node IDs. An empty `NodeIDs` returns the full set.
type GetCurrentValidatorsRequest struct {
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

// GetCurrentValidatorsResponse is the response for
// `validators.getCurrentValidators`.
type GetCurrentValidatorsResponse struct {
	Validators []CurrentValidator `json:"validators"`
}

// NewValidatorsAPI returns the gorilla-rpc service bound to the
// supplied VM dependencies. Register the returned `*ValidatorsAPI`
// under the `validators` namespace via `graft/evm/utils/rpc.NewHandler`
// (or any compatible gorilla-rpc helper).
//
// `connected` is typically `*sae.VM.ValidatorPeers`; `uptime` is
// `*uptimetracker.UptimeTracker` (or any implementer of
// `UptimeSource`). The accessors hold no internal lock - SAE-side
// resources serialise themselves - unlike the legacy subnet-evm
// wrapper which RLocks the VM around each call.
func NewValidatorsAPI(
	validatorState avagovalidators.State,
	subnetID ids.ID,
	uptime UptimeSource,
	connected *p2p.Validators,
) *ValidatorsAPI {
	return &ValidatorsAPI{
		validatorState: validatorState,
		subnetID:       subnetID,
		uptime:         uptime,
		connected:      connected,
	}
}

// ValidatorsAPI is the gorilla-rpc service that exposes
// `validators.getCurrentValidators`. Construct via
// [NewValidatorsAPI].
type ValidatorsAPI struct {
	validatorState avagovalidators.State
	subnetID       ids.ID
	uptime         UptimeSource
	connected      *p2p.Validators
}

// GetCurrentValidators returns the configured validator set, optionally
// filtered to `req.NodeIDs`, augmented with per-validation uptime and
// connection status.
//
// The wire shape (request, reply, JSON tags, and the 0-100 percentage
// scale) is identical to legacy subnet-evm so existing tooling needs no
// changes.
func (api *ValidatorsAPI) GetCurrentValidators(
	httpReq *http.Request,
	req *GetCurrentValidatorsRequest,
	reply *GetCurrentValidatorsResponse,
) error {
	ctx := httpReq.Context()

	validatorSet, _, err := api.validatorState.GetCurrentValidatorSet(ctx, api.subnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	requestedNodeIDs := set.NewSet[ids.NodeID](len(req.NodeIDs))
	for _, nodeID := range req.NodeIDs {
		requestedNodeIDs.Add(nodeID)
	}

	reply.Validators = make([]CurrentValidator, 0, len(validatorSet))
	for validationID, validator := range validatorSet {
		if requestedNodeIDs.Len() > 0 && !requestedNodeIDs.Contains(validator.NodeID) {
			continue
		}

		upDuration, lastUpdated, err := api.uptime.GetUptime(validationID)
		if err != nil {
			return fmt.Errorf("failed to get uptime for validation ID %s: %w", validationID, err)
		}

		var uptimeFloat float64
		startTime := time.Unix(int64(validator.StartTime), 0)
		bestPossibleUpDuration := lastUpdated.Sub(startTime)
		if bestPossibleUpDuration == 0 {
			uptimeFloat = 1
		} else {
			uptimeFloat = float64(upDuration) / float64(bestPossibleUpDuration)
		}
		// 0-100 percentage to match PlatformVM's currentValidators.
		uptimePercentage := float32(uptimeFloat * 100)
		uptimeSeconds := uint64(upDuration.Seconds())

		reply.Validators = append(reply.Validators, CurrentValidator{
			ValidationID:     validationID,
			NodeID:           validator.NodeID,
			StartTimestamp:   validator.StartTime,
			Weight:           validator.Weight,
			IsActive:         validator.IsActive,
			IsL1Validator:    validator.IsL1Validator,
			IsConnected:      api.connected.Has(ctx, validator.NodeID),
			UptimePercentage: uptimePercentage,
			UptimeSeconds:    uptimeSeconds,
		})
	}
	return nil
}
