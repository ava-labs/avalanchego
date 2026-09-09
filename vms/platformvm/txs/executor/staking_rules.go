// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

// GetTransformSubnetTx returns the TransformSubnetTx that transformed
// subnetID, if any.
func GetTransformSubnetTx(chain state.Chain, subnetID ids.ID) (*txs.TransformSubnetTx, error) {
	transformSubnetIntf, err := chain.GetSubnetTransformation(subnetID)
	if err != nil {
		return nil, err
	}

	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return nil, fmt.Errorf("expected tx type *txs.TransformSubnetTx but got %T", transformSubnetIntf.Unsigned)
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
