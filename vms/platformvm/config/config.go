// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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

// Struct collecting all foundational parameters of PlatformVM
type Config struct {
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

	// Fee that must be burned by every create staker transaction
	AddStakerTxFee uint64

	// Fee that is burned by every non-state creating transaction
	TxFee uint64

	// Fee that must be burned by every state creating transaction before AP3
	CreateAssetTxFee uint64

	// Fee that must be burned by every subnet creating transaction after AP3
	CreateSubnetTxFee uint64

	// Fee that must be burned by every blockchain creating transaction after AP3
	CreateBlockchainTxFee uint64

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

	// Time of the AP4 network upgrade
	ApricotPhase4Time time.Time

	// Time of the AP5 network upgrade
	ApricotPhase5Time time.Time
}

func (c *Config) GetCreateBlockchainTxFee(t time.Time) uint64 {
	if t.Before(c.ApricotPhase3Time) {
		return c.CreateAssetTxFee
	}
	return c.CreateBlockchainTxFee
}

func (c *Config) GetCreateSubnetTxFee(t time.Time) uint64 {
	if t.Before(c.ApricotPhase3Time) {
		return c.CreateAssetTxFee
	}
	return c.CreateSubnetTxFee
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
		VMAlias:     tx.VMID.String(),
	}
	for _, fxID := range tx.FxIDs {
		chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
	}
	c.Chains.CreateChain(chainParams)
}
