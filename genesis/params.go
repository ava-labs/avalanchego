// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
)

type Params struct {
	// Transaction fee
	TxFee uint64 `json:"txFee"`
	// Transaction fee for transactions that create new state
	CreationTxFee uint64 `json:"creationTxFee"`
	// Staking uptime requirements
	UptimeRequirement float64 `json:"uptimeRequirement"`
	// Minimum stake, in nAVAX, required to validate the primary network
	MinValidatorStake uint64 `json:"minValidatorStake"`
	// Maximum stake, in nAVAX, allowed to be placed on a single validator in
	// the primary network
	MaxValidatorStake uint64 `json:"maxValidatorStake"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake uint64 `json:"minDelegatorStake"`
	// Minimum delegation fee, in the range [0, 1000000], that can be charged
	// for delegation on the primary network.
	MinDelegationFee uint32 `json:"minDelegationFee"`
	// MinStakeDuration is the minimum amount of time a validator can validate
	// for in a single period.
	MinStakeDuration time.Duration `json:"minStakeDuration"`
	// MaxStakeDuration is the maximum amount of time a validator can validate
	// for in a single period.
	MaxStakeDuration time.Duration `json:"maxStakeDuration"`
	// StakeMintingPeriod is the amount of time for a consumption period.
	StakeMintingPeriod time.Duration `json:"stakeMintingPeriod"`
	// EpochFirstTransition is the time that the transition from epoch 0 to 1
	// should occur.
	EpochFirstTransition time.Time `json:"epochFirstTransition"`
	// EpochDuration is the amount of time that an epoch runs for.
	EpochDuration time.Duration `json:"epochDuration"`
}

func GetParams(networkID uint32) *Params {
	switch networkID {
	case constants.MainnetID:
		return &MainnetParams
	case constants.FujiID:
		return &FujiParams
	case constants.LocalID:
		return &LocalParams
	default:
		return &LocalParams
	}
}
