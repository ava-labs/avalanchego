// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/types"
)

// Staker is the representation of a staker sent via APIs.
// [TxID] is the txID of the transaction that added this staker.
// [Endtime] is the Unix time repr. of when they are done staking
// [NodeID] is the node ID of the staker
// [Weight] is the validator weight (stake) when sampling validators
type Staker struct {
	TxID      ids.ID      `json:"txID"`
	StartTime json.Uint64 `json:"startTime"`
	EndTime   json.Uint64 `json:"endTime"`
	Weight    json.Uint64 `json:"weight"`
	NodeID    ids.NodeID  `json:"nodeID"`
}

// Owner is the repr. of a reward owner sent over APIs.
type Owner struct {
	Locktime  json.Uint64 `json:"locktime"`
	Threshold json.Uint32 `json:"threshold"`
	Addresses []string    `json:"addresses"`
}

// PrimaryDelegator is the repr. of a primary network delegator sent over APIs.
type PrimaryDelegator struct {
	Staker
	RewardOwner     *Owner       `json:"rewardOwner,omitempty"`
	PotentialReward *json.Uint64 `json:"potentialReward,omitempty"`
}

// UTXO is a UTXO on the Platform Chain that exists at the chain's genesis.
type UTXO struct {
	Locktime json.Uint64 `json:"locktime"`
	Amount   json.Uint64 `json:"amount"`
	Address  string      `json:"address"`
	Message  string      `json:"message"`
}

// APIL1Validator is the representation of a L1 validator sent over APIs.
type APIL1Validator struct {
	NodeID    ids.NodeID  `json:"nodeID"`
	Weight    json.Uint64 `json:"weight"`
	StartTime json.Uint64 `json:"startTime"`
	BaseL1Validator
}

// BaseL1Validator is the representation of a base L1 validator without the common parts with a staker.
type BaseL1Validator struct {
	ValidationID *ids.ID `json:"validationID,omitempty"`
	// PublicKey is the compressed BLS public key of the validator
	PublicKey             *types.JSONByteSlice `json:"publicKey,omitempty"`
	RemainingBalanceOwner *Owner               `json:"remainingBalanceOwner,omitempty"`
	DeactivationOwner     *Owner               `json:"deactivationOwner,omitempty"`
	MinNonce              *json.Uint64         `json:"minNonce,omitempty"`
	// Balance is the remaining amount of AVAX this L1 validator has for paying
	// the continuous fee, according to the last accepted state. If the
	// validator is inactive, the balance will be 0.
	Balance *json.Uint64 `json:"balance,omitempty"`
}

// PermissionlessValidator is the repr. of a permissionless validator sent over
// APIs.
type PermissionlessValidator struct {
	Staker
	BaseL1Validator
	// The owner of the rewards from the validation period, if applicable.
	ValidationRewardOwner *Owner `json:"validationRewardOwner,omitempty"`
	// The owner of the rewards from delegations during the validation period,
	// if applicable.
	DelegationRewardOwner  *Owner                    `json:"delegationRewardOwner,omitempty"`
	PotentialReward        *json.Uint64              `json:"potentialReward,omitempty"`
	AccruedDelegateeReward *json.Uint64              `json:"accruedDelegateeReward,omitempty"`
	DelegationFee          json.Float32              `json:"delegationFee"`
	ExactDelegationFee     *json.Uint32              `json:"exactDelegationFee,omitempty"`
	Uptime                 *json.Float32             `json:"uptime,omitempty"`
	Connected              *bool                     `json:"connected,omitempty"`
	Staked                 []UTXO                    `json:"staked,omitempty"`
	Signer                 *signer.ProofOfPossession `json:"signer,omitempty"`

	// The delegators delegating to this validator
	DelegatorCount  *json.Uint64        `json:"delegatorCount,omitempty"`
	DelegatorWeight *json.Uint64        `json:"delegatorWeight,omitempty"`
	Delegators      *[]PrimaryDelegator `json:"delegators,omitempty"`
}
