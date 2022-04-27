// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	addressconverter "github.com/ava-labs/avalanchego/utils/formatting/addressconverter"
)

// ClientStaker is the representation of a staker sent via client.
// [TxID] is the txID of the transaction that added this staker.
// [Amount] is the amount of tokens being staked.
// [StartTime] is the Unix time when they start staking
// [Endtime] is the Unix time repr. of when they are done staking
// [NodeID] is the node ID of the staker
type ClientStaker struct {
	TxID        ids.ID
	StartTime   uint64
	EndTime     uint64
	Weight      *uint64
	StakeAmount *uint64
	NodeID      ids.ShortID
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

func APIStakerToClientStaker(validator APIStaker) (ClientStaker, error) {
	var err error
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
	clientStaker.NodeID, err = ids.ShortFromPrefixedString(validator.NodeID, constants.NodeIDPrefix)
	if err != nil {
		return ClientStaker{}, err
	}
	return clientStaker, nil
}

func APIOwnerToClientOwner(rewardOwner *APIOwner) (*ClientOwner, error) {
	if rewardOwner == nil {
		return nil, nil
	}
	var err error
	var clientOwner ClientOwner
	clientOwner.Locktime = uint64(rewardOwner.Locktime)
	clientOwner.Threshold = uint32(rewardOwner.Threshold)
	clientOwner.Addresses, err = addressconverter.ParseAddressesToID(rewardOwner.Addresses)
	if err != nil {
		return nil, err
	}
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
		clientValidators[i].ClientStaker, err = APIStakerToClientStaker(validator.APIStaker)
		if err != nil {
			return nil, err
		}
		clientValidators[i].RewardOwner, err = APIOwnerToClientOwner(validator.RewardOwner)
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
			clientValidators[i].Delegators[j].ClientStaker, err = APIStakerToClientStaker(delegator.APIStaker)
			if err != nil {
				return nil, err
			}
			clientValidators[i].Delegators[j].RewardOwner, err = APIOwnerToClientOwner(delegator.RewardOwner)
			if err != nil {
				return nil, err
			}
			if delegator.PotentialReward != nil {
				v := uint64(*delegator.PotentialReward)
				clientValidators[i].Delegators[j].PotentialReward = &v
			}
		}
	}
	return clientValidators, nil
}
