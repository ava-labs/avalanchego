// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package configtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

// Shared Unit test setup utilities for a platform vm packages

type ActiveFork uint8

const (
	ApricotPhase3Fork ActiveFork = iota
	ApricotPhase5Fork
	BanffFork
	CortinaFork
	DurangoFork

	LatestFork ActiveFork = DurangoFork
)

var (
	MinStakingDuration = 24 * time.Hour
	MaxStakingDuration = 365 * 24 * time.Hour

	MinValidatorStake = 5 * units.MilliAvax
	MaxValidatorStake = 500 * units.MilliAvax

	Balance = 100 * MinValidatorStake
	Weight  = MinValidatorStake

	TxFee = uint64(100) // a default Tx Fee
)

type MutableSharedMemory struct {
	atomic.SharedMemory
}

func Context(tb testing.TB, baseDB database.Database) (*snow.Context, *MutableSharedMemory) {
	ctx := snowtest.Context(tb, snowtest.PChainID)
	m := atomic.NewMemory(baseDB)
	msm := &MutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	return ctx, msm
}

func Config(fork ActiveFork, forkTime time.Time) *config.Config {
	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		durangoTime       = mockable.MaxTime
	)

	switch fork {
	case DurangoFork:
		durangoTime = forkTime
		fallthrough
	case CortinaFork:
		cortinaTime = forkTime
		fallthrough
	case BanffFork:
		banffTime = forkTime
		fallthrough
	case ApricotPhase5Fork:
		apricotPhase5Time = forkTime
		fallthrough
	case ApricotPhase3Fork:
		apricotPhase3Time = forkTime
	default:
		panic(fmt.Errorf("unhandled fork %d", fork))
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
		ApricotPhase3Time: apricotPhase3Time,
		ApricotPhase5Time: apricotPhase5Time,
		BanffTime:         banffTime,
		CortinaTime:       cortinaTime,
		DurangoTime:       durangoTime,
	}
}
