// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/chain4travel/caminogo/chains"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow"
	"github.com/chain4travel/caminogo/snow/uptime"
	"github.com/chain4travel/caminogo/snow/validators"
	"github.com/chain4travel/caminogo/vms/platformvm/reward"
)

// Factory can create new instances of the Platform Chain
type Factory struct {
	// The node's chain manager
	Chains chains.Manager

	// Node's validator set maps subnetID -> validators of the subnet
	Validators validators.Manager

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

// New returns a new instance of the Platform Chain
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{Factory: *f}, nil
}
