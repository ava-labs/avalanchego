// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var TxFee = uint64(100) // a default Tx Fee

func Config(fork ActiveFork) *config.Config {
	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		dTime             = mockable.MaxTime
	)

	switch fork {
	case ApricotPhase3Fork:
		apricotPhase3Time = forkTimes[ApricotPhase3Fork]
	case ApricotPhase5Fork:
		apricotPhase5Time = forkTimes[ApricotPhase5Fork]
		apricotPhase3Time = forkTimes[ApricotPhase3Fork]
	case BanffFork:
		banffTime = forkTimes[BanffFork]
		apricotPhase5Time = forkTimes[ApricotPhase5Fork]
		apricotPhase3Time = forkTimes[ApricotPhase3Fork]
	case CortinaFork:
		cortinaTime = forkTimes[CortinaFork]
		banffTime = forkTimes[BanffFork]
		apricotPhase5Time = forkTimes[ApricotPhase5Fork]
		apricotPhase3Time = forkTimes[ApricotPhase3Fork]
	case DFork:
		dTime = forkTimes[DFork]
		cortinaTime = forkTimes[CortinaFork]
		banffTime = forkTimes[BanffFork]
		apricotPhase5Time = forkTimes[ApricotPhase5Fork]
		apricotPhase3Time = forkTimes[ApricotPhase3Fork]
	default:
		panic(fmt.Errorf("unhandled fork %d", fork))
	}

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)

	return &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             vdrs,
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
		ApricotPhase3Time: apricotPhase3Time,
		ApricotPhase5Time: apricotPhase5Time,
		BanffTime:         banffTime,
		CortinaTime:       cortinaTime,
		DTime:             dTime,
	}
}
