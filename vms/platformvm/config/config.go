// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

type TxFees struct {
	// Transaction fee
	TxFee uint64 `json:"txFee"`
	// Transaction fee for create asset transactions
	CreateAssetTxFee uint64 `json:"createAssetTxFee"`
	// Transaction fee for create subnet transactions
	CreateSubnetTxFee uint64 `json:"createSubnetTxFee"`
	// Transaction fee for transform subnet transactions
	TransformSubnetTxFee uint64 `json:"transformSubnetTxFee"`
	// Transaction fee for create blockchain transactions
	CreateBlockchainTxFee uint64 `json:"createBlockchainTxFee"`
	// Transaction fee for adding a primary network validator
	AddPrimaryNetworkValidatorFee uint64 `json:"addPrimaryNetworkValidatorFee"`
	// Transaction fee for adding a primary network delegator
	AddPrimaryNetworkDelegatorFee uint64 `json:"addPrimaryNetworkDelegatorFee"`
	// Transaction fee for adding a subnet validator
	AddSubnetValidatorFee uint64 `json:"addSubnetValidatorFee"`
	// Transaction fee for adding a subnet delegator
	AddSubnetDelegatorFee uint64 `json:"addSubnetDelegatorFee"`
}

// Struct collecting all foundational parameters of PlatformVM
type Config struct {
	TxFees

	// The node's chain manager
	Chains chains.Manager

	// Node's validator set maps subnetID -> validators of the subnet
	Validators validators.Manager

	// Provides access to subnet tracking
	SubnetTracker common.SubnetTracker

	// Provides access to the uptime manager as a thread safe data structure
	UptimeLockedCalculator uptime.LockedCalculator

	// True if the node is being run with staking enabled
	StakingEnabled bool

	// Set of subnets that this node is validating
	WhitelistedSubnets ids.Set

	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake uint64

	// The maximum amount of tokens that can be bonded on a validator
	MaxValidatorStake uint64

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake uint64

	// Minimum fee that can be charged for delegation
	MinDelegationFee uint32

	// UptimePercentage is the minimum uptime required to be rewarded for staking
	UptimePercentage float64

	// Minimum amount of time to allow a staker to stake
	MinStakeDuration time.Duration

	// Maximum amount of time to allow a staker to stake
	MaxStakeDuration time.Duration

	// Config for the minting function
	RewardConfig reward.Config

	// Time of the AP3 network upgrade
	ApricotPhase3Time time.Time

	// Time of the AP5 network upgrade
	ApricotPhase5Time time.Time

	// Time of the Blueberry network upgrade
	BlueberryTime time.Time
}

func (c *Config) IsApricotPhase3Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase3Time)
}

func (c *Config) IsApricotPhase5Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase5Time)
}

func (c *Config) IsBlueberryActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.BlueberryTime)
}

func (c *Config) GetCreateBlockchainTxFee(timestamp time.Time) uint64 {
	if c.IsApricotPhase3Activated(timestamp) {
		return c.CreateBlockchainTxFee
	}
	return c.CreateAssetTxFee
}

func (c *Config) GetCreateSubnetTxFee(timestamp time.Time) uint64 {
	if c.IsApricotPhase3Activated(timestamp) {
		return c.CreateSubnetTxFee
	}
	return c.CreateAssetTxFee
}

// Create the blockchain described in [tx], but only if this node is a member of
// the subnet that validates the chain
func (c *Config) CreateChain(chainID ids.ID, tx *txs.CreateChainTx) {
	if c.StakingEnabled && // Staking is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != tx.SubnetID && // All nodes must validate the primary network
		!c.WhitelistedSubnets.Contains(tx.SubnetID) { // This node doesn't validate this blockchain
		return
	}

	c.Chains.CreateChain(chains.ChainParameters{
		ID:          chainID,
		SubnetID:    tx.SubnetID,
		GenesisData: tx.GenesisData,
		VMID:        tx.VMID,
		FxIDs:       tx.FxIDs,
	})
}
