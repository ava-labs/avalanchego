// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
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
	Weight uint64
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

// ClientPermissionlessValidator is the repr. of a permissionless validator sent
// over client
type ClientPermissionlessValidator struct {
	ClientStaker
	ValidationRewardOwner  *ClientOwner
	DelegationRewardOwner  *ClientOwner
	PotentialReward        *uint64
	AccruedDelegateeReward *uint64
	DelegationFee          float32
	// Uptime is deprecated for Subnet Validators.
	// It will be available only for Primary Network Validators.
	Uptime *float32
	// Connected is deprecated for Subnet Validators.
	// It will be available only for Primary Network Validators.
	Connected *bool
	Signer    *signer.ProofOfPossession
	// The delegators delegating to this validator
	DelegatorCount  *uint64
	DelegatorWeight *uint64
	Delegators      []ClientDelegator
}

// ClientDelegator is the repr. of a delegator sent over client
type ClientDelegator struct {
	ClientStaker
	RewardOwner     *ClientOwner
	PotentialReward *uint64
}

func apiStakerToClientStaker(validator api.Staker) ClientStaker {
	return ClientStaker{
		TxID:        validator.TxID,
		StartTime:   uint64(validator.StartTime),
		EndTime:     uint64(validator.EndTime),
		Weight:      uint64(validator.Weight),
		StakeAmount: (*uint64)(validator.StakeAmount),
		NodeID:      validator.NodeID,
	}
}

func apiOwnerToClientOwner(rewardOwner *api.Owner) (*ClientOwner, error) {
	if rewardOwner == nil {
		return nil, nil
	}

	addrs, err := address.ParseToIDs(rewardOwner.Addresses)
	return &ClientOwner{
		Locktime:  uint64(rewardOwner.Locktime),
		Threshold: uint32(rewardOwner.Threshold),
		Addresses: addrs,
	}, err
}

func getClientPermissionlessValidators(validatorsSliceIntf []interface{}) ([]ClientPermissionlessValidator, error) {
	clientValidators := make([]ClientPermissionlessValidator, len(validatorsSliceIntf))
	for i, validatorMapIntf := range validatorsSliceIntf {
		validatorMapJSON, err := json.Marshal(validatorMapIntf)
		if err != nil {
			return nil, err
		}

		var apiValidator api.PermissionlessValidator
		err = json.Unmarshal(validatorMapJSON, &apiValidator)
		if err != nil {
			return nil, err
		}

		validationRewardOwner, err := apiOwnerToClientOwner(apiValidator.ValidationRewardOwner)
		if err != nil {
			return nil, err
		}

		delegationRewardOwner, err := apiOwnerToClientOwner(apiValidator.DelegationRewardOwner)
		if err != nil {
			return nil, err
		}

		var clientDelegators []ClientDelegator
		if apiValidator.Delegators != nil {
			clientDelegators = make([]ClientDelegator, len(*apiValidator.Delegators))
			for j, apiDelegator := range *apiValidator.Delegators {
				rewardOwner, err := apiOwnerToClientOwner(apiDelegator.RewardOwner)
				if err != nil {
					return nil, err
				}

				clientDelegators[j] = ClientDelegator{
					ClientStaker:    apiStakerToClientStaker(apiDelegator.Staker),
					RewardOwner:     rewardOwner,
					PotentialReward: (*uint64)(apiDelegator.PotentialReward),
				}
			}
		}

		clientValidators[i] = ClientPermissionlessValidator{
			ClientStaker:           apiStakerToClientStaker(apiValidator.Staker),
			ValidationRewardOwner:  validationRewardOwner,
			DelegationRewardOwner:  delegationRewardOwner,
			PotentialReward:        (*uint64)(apiValidator.PotentialReward),
			AccruedDelegateeReward: (*uint64)(apiValidator.AccruedDelegateeReward),
			DelegationFee:          float32(apiValidator.DelegationFee),
			Uptime:                 (*float32)(apiValidator.Uptime),
			Connected:              apiValidator.Connected,
			Signer:                 apiValidator.Signer,
			DelegatorCount:         (*uint64)(apiValidator.DelegatorCount),
			DelegatorWeight:        (*uint64)(apiValidator.DelegatorWeight),
			Delegators:             clientDelegators,
		}
	}
	return clientValidators, nil
}
