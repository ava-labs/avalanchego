// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Struct collecting all foundational parameters of PlatformVM
type Config struct {
	// The node's chain manager
	Chains chains.Manager

	// Node's validator set maps subnetID -> validators of the subnet
	//
	// Invariant: The primary network's validator set should have been added to
	//            the manager before calling VM.Initialize.
	// Invariant: The primary network's validator set should be empty before
	//            calling VM.Initialize.
	Validators validators.Manager

	// Provides access to the uptime manager as a thread safe data structure
	UptimeLockedCalculator uptime.LockedCalculator

	// True if the node is being run with staking enabled
	StakingEnabled bool

	// Set of subnets that this node is validating
	WhitelistedSubnets set.Set[ids.ID]

	// Fee that is burned by every non-state creating transaction
	TxFee uint64

	// Fee that must be burned by every state creating transaction before AP3
	CreateAssetTxFee uint64

	// Fee that must be burned by every subnet creating transaction after AP3
	CreateSubnetTxFee uint64

	// Fee that must be burned by every transform subnet transaction
	TransformSubnetTxFee uint64

	// Fee that must be burned by every blockchain creating transaction after AP3
	CreateBlockchainTxFee uint64

	// Transaction fee for adding a primary network validator
	AddPrimaryNetworkValidatorFee uint64

	// Transaction fee for adding a primary network delegator
	AddPrimaryNetworkDelegatorFee uint64

	// Transaction fee for adding a subnet validator
	AddSubnetValidatorFee uint64

	// Transaction fee for adding a subnet delegator
	AddSubnetDelegatorFee uint64

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

	// Time of the Banff network upgrade
	BanffTime time.Time

	// Subnet ID --> Minimum portion of the subnet's stake this node must be
	// connected to in order to report healthy.
	// [constants.PrimaryNetworkID] is always a key in this map.
	// If a subnet is in this map, but it isn't whitelisted, its corresponding
	// value isn't used.
	// If a subnet is whitelisted but not in this map, we use the value for the
	// Primary Network.
	MinPercentConnectedStakeHealthy map[ids.ID]float64

	// UseCurrentHeight forces [GetMinimumHeight] to return the current height
	// of the P-Chain instead of the oldest block in the [recentlyAccepted]
	// window.
	//
	// This config is particularly useful for triggering proposervm activation
	// on recently created subnets (without this, users need to wait for
	// [recentlyAcceptedWindowTTL] to pass for activation to occur).
	UseCurrentHeight bool
}

func (c *Config) IsApricotPhase3Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase3Time)
}

func (c *Config) IsApricotPhase5Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase5Time)
}

func (c *Config) IsBanffActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.BanffTime)
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

	chainParams := chains.ChainParameters{
		ID:          chainID,
		SubnetID:    tx.SubnetID,
		GenesisData: tx.GenesisData,
		VMID:        tx.VMID,
		FxIDs:       tx.FxIDs,
	}

	c.Chains.QueueChainCreation(chainParams)
}
