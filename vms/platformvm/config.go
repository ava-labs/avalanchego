// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type Config struct {
	// True if the node is being run with staking enabled
	StakingEnabled bool

	// Node's validator set maps subnetID -> validators of the subnet
	Validators validators.Manager

	// The node's chain manager
	Chains chains.Manager

	// Fee that is burned by every non-state creating transaction
	TxFee uint64

	// fee that must be burned by every state creating transaction
	CreationTxFee uint64

	// UptimePercentage is the minimum uptime required to be rewarded for staking.
	UptimePercentage float64

	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake uint64

	// The maximum amount of tokens one can bond to a validator
	MaxValidatorStake uint64

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake uint64

	// Minimum fee that can be charged for delegation
	MinDelegationFee uint32

	// Minimum amount of time to allow a validator to stake
	MinStakeDuration time.Duration

	// Maximum amount of time to allow a validator to stake
	MaxStakeDuration time.Duration

	// Consumption period for the minting function
	StakeMintingPeriod time.Duration

	// Time of the apricot phase 0 rule change
	ApricotPhase0Time time.Time
}
