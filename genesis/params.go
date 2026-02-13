// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

var (
	errInvalidUptimeRequirement           = errors.New("uptime requirement value must be in the range [0, 1]")
	errUptimeRequirementTimeNotIncreasing = errors.New("uptime requirement schedule times must be strictly increasing")
)

// UptimeRequirementUpdate defines an update to primary network
// uptime requirements. As of Time, Requirement is the new
// minimum uptime percentage required to prefer rewarding a
// given staker.
type UptimeRequirementUpdate struct {
	Time        time.Time `json:"time"`
	Requirement float64   `json:"requirement"`
}

// UptimeRequirementConfig defines the uptime requirements for primary
// network validators to receive rewards.
type UptimeRequirementConfig struct {
	// DefaultRequiredUptimePercentage is the default uptime required to be rewarded for staking.
	DefaultRequiredUptimePercentage float64 `json:"defaultRequiredUptimePercentage"`

	// RequiredUptimePercentageSchedule is the minimum uptime required to be rewarded for staking.
	// The Time of each UptimeRequirementUpdate must be strictly after its predecessor.
	RequiredUptimePercentageSchedule []UptimeRequirementUpdate `json:"requiredUptimePercentageSchedule"`
}

func (c *UptimeRequirementConfig) Verify() error {
	// Requirement value must be in [0, 1].
	if c.DefaultRequiredUptimePercentage < 0 || c.DefaultRequiredUptimePercentage > 1 {
		return errInvalidUptimeRequirement
	}

	for i, update := range c.RequiredUptimePercentageSchedule {
		// Requirement value must be in [0, 1].
		if update.Requirement < 0 || update.Requirement > 1 {
			return fmt.Errorf("%w at index %d: %f", errInvalidUptimeRequirement, i, update.Requirement)
		}

		// Times must be strictly increasing.
		if i > 0 && !update.Time.After(c.RequiredUptimePercentageSchedule[i-1].Time) {
			return fmt.Errorf("%w at index %d: %s is not after %s",
				errUptimeRequirementTimeNotIncreasing,
				i,
				update.Time,
				c.RequiredUptimePercentageSchedule[i-1].Time,
			)
		}
	}

	return nil
}

// RequiredUptime determines the required uptime to prefer rewarding a primary
// network staker given the staker's start time. The requirement of the latest
// UptimeRequirementUpdate to take effect at time before the stakerStartTime
// is applied, if any exist.
func (c *UptimeRequirementConfig) RequiredUptime(stakerStartTime time.Time) float64 {
	requiredUptime := c.DefaultRequiredUptimePercentage
	for _, requiredUptimeUpdate := range c.RequiredUptimePercentageSchedule {
		if stakerStartTime.Before(requiredUptimeUpdate.Time) {
			return requiredUptime
		}
		requiredUptime = requiredUptimeUpdate.Requirement
	}
	return requiredUptime
}

type StakingConfig struct {
	// Staking uptime requirements
	UptimeRequirementConfig UptimeRequirementConfig `json:"uptimeRequirementConfig"`
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
	// RewardConfig is the config for the reward function.
	RewardConfig reward.Config `json:"rewardConfig"`
}

type TxFeeConfig struct {
	CreateAssetTxFee   uint64     `json:"createAssetTxFee"`
	TxFee              uint64     `json:"txFee"`
	DynamicFeeConfig   gas.Config `json:"dynamicFeeConfig"`
	ValidatorFeeConfig fee.Config `json:"validatorFeeConfig"`
}

type Params struct {
	StakingConfig
	TxFeeConfig
}

func GetTxFeeConfig(networkID uint32) TxFeeConfig {
	switch networkID {
	case constants.MainnetID:
		return MainnetParams.TxFeeConfig
	case constants.FujiID:
		return FujiParams.TxFeeConfig
	case constants.LocalID:
		return LocalParams.TxFeeConfig
	default:
		return LocalParams.TxFeeConfig
	}
}

func GetStakingConfig(networkID uint32) StakingConfig {
	switch networkID {
	case constants.MainnetID:
		return MainnetParams.StakingConfig
	case constants.FujiID:
		return FujiParams.StakingConfig
	case constants.LocalID:
		return LocalParams.StakingConfig
	default:
		return LocalParams.StakingConfig
	}
}
