// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

// ClientStaker is the representation of a staker sent via client.
type ClientStaker struct {
	// the txID of the transaction that added this staker.
	TxID ids.ID
	// the Unix time when they start staking
	StartTime uint64
	// the Unix time when they are done staking
	EndTime uint64
	// the validator weight when sampling validators
	Weight *uint64
	// the amount of tokens being staked.
	StakeAmount *uint64
	// the node ID of the staker
	NodeID ids.NodeID
}

// ClientOwner is the repr. of a reward owner sent over client
type ClientOwner struct {
	Locktime  uint64
	Threshold uint32
	Addresses []ids.ShortID
}

// ClientPrimaryValidator is the repr. of a primary network validator sent over client
type ClientPrimaryValidator struct {
	ClientStaker
	// The owner the staking reward, if applicable, will go to
	RewardOwner     *ClientOwner
	PotentialReward *uint64
	DelegationFee   float32
	Uptime          *float32
	Connected       *bool
	// The delegators delegating to this validator
	Delegators []ClientPrimaryDelegator
}

// ClientPrimaryDelegator is the repr. of a primary network delegator sent over client
type ClientPrimaryDelegator struct {
	ClientStaker
	RewardOwner     *ClientOwner
	PotentialReward *uint64
}

func apiStakerToClientStaker(validator APIStaker) ClientStaker {
	var clientStaker ClientStaker
	clientStaker.TxID = validator.TxID
	clientStaker.StartTime = uint64(validator.StartTime)
	clientStaker.EndTime = uint64(validator.EndTime)
	if validator.Weight != nil {
		v := uint64(*validator.Weight)
		clientStaker.Weight = &v
	}
	if validator.StakeAmount != nil {
		v := uint64(*validator.StakeAmount)
		clientStaker.StakeAmount = &v
	}
	clientStaker.NodeID = validator.NodeID
	return clientStaker
}

func apiOwnerToClientOwner(rewardOwner *APIOwner) (*ClientOwner, error) {
	if rewardOwner == nil {
		return nil, nil
	}
	var (
		err         error
		clientOwner ClientOwner
	)
	clientOwner.Locktime = uint64(rewardOwner.Locktime)
	clientOwner.Threshold = uint32(rewardOwner.Threshold)
	clientOwner.Addresses, err = address.ParseAddressesToID(rewardOwner.Addresses)
	return &clientOwner, err
}

func getClientPrimaryValidators(validatorsSliceIntf []interface{}) ([]ClientPrimaryValidator, error) {
	clientValidators := make([]ClientPrimaryValidator, len(validatorsSliceIntf))
	for i, validatorMapIntf := range validatorsSliceIntf {
		b, err := json.Marshal(validatorMapIntf)
		if err != nil {
			return nil, err
		}
		var validator APIPrimaryValidator
		err = json.Unmarshal(b, &validator)
		if err != nil {
			return nil, err
		}
		clientValidators[i].ClientStaker = apiStakerToClientStaker(validator.APIStaker)
		clientValidators[i].RewardOwner, err = apiOwnerToClientOwner(validator.RewardOwner)
		if err != nil {
			return nil, err
		}
		if validator.PotentialReward != nil {
			v := uint64(*validator.PotentialReward)
			clientValidators[i].PotentialReward = &v
		}
		clientValidators[i].DelegationFee = float32(validator.DelegationFee)
		if validator.Uptime != nil {
			v := float32(*validator.Uptime)
			clientValidators[i].Uptime = &v
		}
		if validator.Connected != nil {
			v := *validator.Connected
			clientValidators[i].Connected = &v
		}
		clientValidators[i].Delegators = make([]ClientPrimaryDelegator, len(validator.Delegators))
		for j, delegator := range validator.Delegators {
			clientValidators[i].Delegators[j].ClientStaker = apiStakerToClientStaker(delegator.APIStaker)
			clientValidators[i].Delegators[j].RewardOwner, err = apiOwnerToClientOwner(delegator.RewardOwner)
			if err != nil {
				return nil, err
			}
			v := uint64(*delegator.PotentialReward)
			clientValidators[i].Delegators[j].PotentialReward = &v
		}
	}
	return clientValidators, nil
}
