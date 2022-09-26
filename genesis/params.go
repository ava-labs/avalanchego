// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"

	xchainconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	pchainconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
)

type StakingConfig struct {
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
	// RewardConfig is the config for the reward function.
	RewardConfig reward.Config `json:"rewardConfig"`
}

type Params struct {
	StakingConfig
	PChainTxFees pchainconfig.TxFeeUpgrades
	XChainTxFees xchainconfig.TxFees
}

func GetTxFeeUpgrades(networkID uint32) (pchainconfig.TxFeeUpgrades, xchainconfig.TxFees) {
	switch networkID {
	case constants.MainnetID:
		return MainnetParams.PChainTxFees, MainnetParams.XChainTxFees
	case constants.FujiID:
		return FujiParams.PChainTxFees, FujiParams.XChainTxFees
	case constants.LocalID:
		return LocalParams.PChainTxFees, LocalParams.XChainTxFees
	default:
		return LocalParams.PChainTxFees, LocalParams.XChainTxFees
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
