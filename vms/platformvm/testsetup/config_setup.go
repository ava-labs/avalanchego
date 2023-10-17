// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	TxFee = uint64(100) // a default Tx Fee

	// Many tests add a subnet as soon as they build test env/VM
	// We single out the fee required to create the subnet to ease up
	// balance checks. CreateSubnetTxFee will be subtracted from the
	// initial balance of the key used to create the subnet
	CreateSubnetTxFee = 100 * TxFee
)

func Config(fork ActiveFork, forkTime time.Time) *config.Config {
	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		dTime             = mockable.MaxTime
	)

	switch fork {
	case ApricotPhase3Fork:
		apricotPhase3Time = forkTime
	case ApricotPhase5Fork:
		apricotPhase5Time = forkTime
		apricotPhase3Time = GenesisTime
	case BanffFork:
		banffTime = forkTime
		apricotPhase5Time = GenesisTime
		apricotPhase3Time = GenesisTime
	case CortinaFork:
		cortinaTime = forkTime
		banffTime = GenesisTime
		apricotPhase5Time = GenesisTime
		apricotPhase3Time = GenesisTime
	case DFork:
		dTime = forkTime
		cortinaTime = GenesisTime
		banffTime = GenesisTime
		apricotPhase5Time = GenesisTime
		apricotPhase3Time = GenesisTime
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
