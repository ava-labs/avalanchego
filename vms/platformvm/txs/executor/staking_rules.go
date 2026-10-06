// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// This file resolves the staking rules that apply to a subnet: the primary
// network uses the node's config, while a transformed subnet uses the
// parameters of its TransformSubnetTx.

// primaryNetworkMaxValidatorWeightFactor is the multiple of a primary network
// validator's own stake that its total stake, including delegations, may
// reach.
const primaryNetworkMaxValidatorWeightFactor = 5

type addValidatorRules struct {
	assetID           ids.ID
	minValidatorStake uint64
	maxValidatorStake uint64
	minStakeDuration  time.Duration
	maxStakeDuration  time.Duration
	minDelegationFee  uint32
}

// verifyValidator ensures the weight, delegation shares, and staking duration
// of a validator are within the bounds of r.
func (r *addValidatorRules) verifyValidator(
	weight uint64,
	delegationShares uint32,
	duration time.Duration,
) error {
	switch {
	case weight < r.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall

	case weight > r.maxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge

	case delegationShares < r.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return errInsufficientDelegationFee
	}

	return verifyStakeDuration(duration, r.minStakeDuration, r.maxStakeDuration)
}

// GetTransformSubnetTx returns the TransformSubnetTx that transformed
// subnetID, if any.
func GetTransformSubnetTx(chain state.Chain, subnetID ids.ID) (*platform.TransformSubnetTx, error) {
	transformSubnetIntf, err := chain.GetSubnetTransformation(subnetID)
	if err != nil {
		return nil, err
	}

	transformSubnet, ok := transformSubnetIntf.Unsigned.(*platform.TransformSubnetTx)
	if !ok {
		return nil, fmt.Errorf("expected tx type *platform.TransformSubnetTx but got %T", transformSubnetIntf.Unsigned)
	}

	return transformSubnet, nil
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

// verifyDelegator ensures the weight and staking duration of a delegator are
// within the bounds of r.
func (r *addDelegatorRules) verifyDelegator(weight uint64, duration time.Duration) error {
	if weight < r.minDelegatorStake {
		// Ensure delegator is staking at least the minimum amount
		return errWeightTooSmall
	}

	return verifyStakeDuration(duration, r.minStakeDuration, r.maxStakeDuration)
}

// verifyStakeDuration ensures duration is within minStakeDuration and
// maxStakeDuration, inclusive.
func verifyStakeDuration(duration, minStakeDuration, maxStakeDuration time.Duration) error {
	switch {
	case duration < minStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	return nil
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
			maxValidatorWeightFactor: primaryNetworkMaxValidatorWeightFactor,
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
