// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var TxFee = uint64(100) // a default Tx Fee

func Config(postBanff, postCortina bool) *config.Config {
	forkTime := ValidateEndTime.Add(-2 * time.Second)
	banffTime := mockable.MaxTime
	if postBanff {
		banffTime = ValidateEndTime.Add(-2 * time.Second)
	}
	cortinaTime := mockable.MaxTime
	if postCortina {
		cortinaTime = ValidateStartTime.Add(-2 * time.Second)
	}

	return &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		TxFee:                  TxFee,
		CreateSubnetTxFee:      100 * TxFee,
		TransformSubnetTxFee:   100 * TxFee,
		CreateBlockchainTxFee:  100 * TxFee,
		MinValidatorStake:      MinValidatorStake,
		MaxValidatorStake:      MaxValidatorStake,
		MinDelegatorStake:      1 * units.MilliAvax,
		MinStakeDuration:       MinStakingDuration,
		MaxStakeDuration:       MaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      MaxStakingDuration,
			SupplyCap:          720 * units.MegaAvax,
		},
		ApricotPhase3Time: forkTime,
		ApricotPhase5Time: forkTime,
		BanffTime:         banffTime,
		CortinaTime:       cortinaTime,
		DTime:             mockable.MaxTime,
	}
}
