// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

type addValidatorRules struct {
	assetID           ids.ID
	minValidatorStake uint64
	maxValidatorStake uint64
	minStakeDuration  time.Duration
	maxStakeDuration  time.Duration
	minDelegationFee  uint32
}

func primaryNetworkValidatorMinStakeDuration(cfg *config.Internal, timestamp time.Time) time.Duration {
	if cfg.UpgradeConfig.IsHeliconActivated(timestamp) {
		return cfg.HeliconMinStakeDuration
	}
	return cfg.MinStakeDuration
}

func getValidatorRules(
	backend *Backend,
	chainState state.Chain,
	subnetID ids.ID,
) (*addValidatorRules, error) {
	if subnetID == constants.PrimaryNetworkID {
		return &addValidatorRules{
			assetID:           backend.Ctx.AVAXAssetID,
			minValidatorStake: backend.Config.MinValidatorStake,
			maxValidatorStake: backend.Config.MaxValidatorStake,
			minStakeDuration:  primaryNetworkValidatorMinStakeDuration(backend.Config, chainState.GetTimestamp()),
			maxStakeDuration:  backend.Config.MaxStakeDuration,
			minDelegationFee:  backend.Config.MinDelegationFee,
		}, nil
	}

	transformSubnet, err := GetTransformSubnetTx(chainState, subnetID)
	if err != nil {
		return nil, err
	}

	return &addValidatorRules{
		assetID:           transformSubnet.AssetID,
		minValidatorStake: transformSubnet.MinValidatorStake,
		maxValidatorStake: transformSubnet.MaxValidatorStake,
		minStakeDuration:  time.Duration(transformSubnet.MinStakeDuration) * time.Second,
		maxStakeDuration:  time.Duration(transformSubnet.MaxStakeDuration) * time.Second,
		minDelegationFee:  transformSubnet.MinDelegationFee,
	}, nil
}

type addDelegatorRules struct {
	assetID                  ids.ID
	minDelegatorStake        uint64
	maxValidatorStake        uint64
	minStakeDuration         time.Duration
	maxStakeDuration         time.Duration
	maxValidatorWeightFactor byte
}

func getDelegatorRules(
	backend *Backend,
	chainState state.Chain,
	subnetID ids.ID,
) (*addDelegatorRules, error) {
	if subnetID == constants.PrimaryNetworkID {
		return &addDelegatorRules{
			assetID:                  backend.Ctx.AVAXAssetID,
			minDelegatorStake:        backend.Config.MinDelegatorStake,
			maxValidatorStake:        backend.Config.MaxValidatorStake,
			minStakeDuration:         backend.Config.MinStakeDuration,
			maxStakeDuration:         backend.Config.MaxStakeDuration,
			maxValidatorWeightFactor: MaxValidatorWeightFactor,
		}, nil
	}

	transformSubnet, err := GetTransformSubnetTx(chainState, subnetID)
	if err != nil {
		return nil, err
	}

	return &addDelegatorRules{
		assetID:                  transformSubnet.AssetID,
		minDelegatorStake:        transformSubnet.MinDelegatorStake,
		maxValidatorStake:        transformSubnet.MaxValidatorStake,
		minStakeDuration:         time.Duration(transformSubnet.MinStakeDuration) * time.Second,
		maxStakeDuration:         time.Duration(transformSubnet.MaxStakeDuration) * time.Second,
		maxValidatorWeightFactor: transformSubnet.MaxValidatorWeightFactor,
	}, nil
}

// overDelegated returns true if [validator] will be overdelegated when adding [delegator].
//
// A [validator] would become overdelegated if:
// - the maximum total weight on [validator] exceeds [weightLimit]
func overDelegated(
	state state.Chain,
	validator state.StakingPeriod,
	weightLimit uint64,
	delegatorWeight uint64,
	delegatorStartTime time.Time,
	delegatorEndTime time.Time,
) (bool, error) {
	maxWeight, err := getMaxWeight(
		state,
		validator.SubnetID(),
		validator.NodeID(),
		validator.Weight(),
		delegatorStartTime,
		delegatorEndTime,
	)
	if err != nil {
		return true, err
	}
	newMaxWeight, err := math.Add(maxWeight, delegatorWeight)
	if err != nil {
		return true, err
	}
	return newMaxWeight > weightLimit, nil
}

// getMaxWeight returns the maximum total weight of the validator, including
// its own weight, between [startTime] and [endTime].
// The weight changes are applied in the order they will be applied as chain
// time advances.
// Invariant:
// - [validator.StartTime()] <= [startTime] < [endTime] <= [validator.EndTime()]
func getMaxWeight(
	chainState state.Chain,
	subnetID ids.ID,
	nodeID ids.NodeID,
	validatorWeight uint64,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	typedState := state.NewAdapter(chainState)

	currentDelegators, err := typedState.GetCurrentDelegators(subnetID, nodeID)
	if err != nil {
		return 0, err
	}

	// TODO: We can optimize this by moving the current total weight to be
	//       stored in the validator state.
	//
	// Calculate the current total weight on this validator, including the
	// weight of the actual validator and the sum of the weights of all of the
	// currently active delegators.
	currentWeight := validatorWeight
	for delegator := range currentDelegators {
		currentWeight, err = math.Add(currentWeight, delegator.StakingPeriod().Weight())
		if err != nil {
			return 0, err
		}
	}

	delegatorDiffs, err := typedState.GetDelegatorDiffs(subnetID, nodeID)
	if err != nil {
		return 0, err
	}

	// Iterate over the future stake weight changes and calculate the maximum
	// total weight on the validator, only including the points in the time
	// range [startTime, endTime].
	var currentMax uint64
	for diff := range delegatorDiffs {
		// [diff.Time] > [endTime]
		if diff.Time.After(endTime) {
			// This delegation change (and all following changes) occurs after
			// [endTime]. Since we're calculating the max amount staked in
			// [startTime, endTime], we can stop.
			break
		}

		// [diff.Time] >= [startTime]
		if !diff.Time.Before(startTime) {
			// We have advanced time to be at the inside of the delegation
			// window. Make sure that the max weight is updated accordingly.
			currentMax = max(currentMax, currentWeight)
		}

		var op func(uint64, uint64) (uint64, error)
		if diff.Added {
			op = math.Add
		} else {
			op = math.Sub
		}
		currentWeight, err = op(currentWeight, diff.Period.Weight())
		if err != nil {
			return 0, err
		}
	}
	// Because we assume [startTime] < [endTime], we have advanced time to
	// be at the end of the delegation window. Make sure that the max weight is
	// updated accordingly.
	return max(currentMax, currentWeight), nil
}

func GetTransformSubnetTx(chain state.Chain, subnetID ids.ID) (*platform.TransformSubnetTx, error) {
	transformSubnetIntf, err := chain.GetSubnetTransformation(subnetID)
	if err != nil {
		return nil, err
	}

	transformSubnet, ok := transformSubnetIntf.Unsigned.(*platform.TransformSubnetTx)
	if !ok {
		return nil, ErrIsNotTransformSubnetTx
	}

	return transformSubnet, nil
}
