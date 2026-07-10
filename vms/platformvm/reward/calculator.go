// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ Calculator = (*calculator)(nil)
	_ Calculator = (*primaryNetworkCalculator)(nil)
)

type Calculator interface {
	Calculate(stakeStartTime time.Time, stakedDuration time.Duration, stakedAmount, currentSupply uint64) uint64
}

type calculator struct {
	maxSubMinConsumptionRate *big.Int
	minConsumptionRate       *big.Int
	mintingPeriod            *big.Int
	supplyCap                uint64
}

// NewCalculator returns a calculator for the provided reward config as-is.
// It does not account for reward changes introduced by network upgrades.
func NewCalculator(c Config) Calculator {
	return &calculator{
		maxSubMinConsumptionRate: new(big.Int).SetUint64(c.MaxConsumptionRate - c.MinConsumptionRate),
		minConsumptionRate:       new(big.Int).SetUint64(c.MinConsumptionRate),
		mintingPeriod:            new(big.Int).SetUint64(uint64(c.MintingPeriod)),
		supplyCap:                c.SupplyCap,
	}
}

// Reward returns the amount of tokens to reward the staker with.
//
// RemainingSupply = SupplyCap - ExistingSupply
// PortionOfExistingSupply = StakedAmount / ExistingSupply
// PortionOfStakingDuration = StakingDuration / MaximumStakingDuration
// MintingRate = MinMintingRate + MaxSubMinMintingRate * PortionOfStakingDuration
// Reward = RemainingSupply * PortionOfExistingSupply * MintingRate * PortionOfStakingDuration
func (c *calculator) Calculate(_ time.Time, stakedDuration time.Duration, stakedAmount, currentSupply uint64) uint64 {
	bigStakedDuration := new(big.Int).SetUint64(uint64(stakedDuration))
	bigStakedAmount := new(big.Int).SetUint64(stakedAmount)
	bigCurrentSupply := new(big.Int).SetUint64(currentSupply)

	adjustedConsumptionRateNumerator := new(big.Int).Mul(c.maxSubMinConsumptionRate, bigStakedDuration)
	adjustedMinConsumptionRateNumerator := new(big.Int).Mul(c.minConsumptionRate, c.mintingPeriod)
	adjustedConsumptionRateNumerator.Add(adjustedConsumptionRateNumerator, adjustedMinConsumptionRateNumerator)
	adjustedConsumptionRateDenominator := new(big.Int).Mul(c.mintingPeriod, consumptionRateDenominator)

	remainingSupply := c.supplyCap - currentSupply
	reward := new(big.Int).SetUint64(remainingSupply)
	reward.Mul(reward, adjustedConsumptionRateNumerator)
	reward.Mul(reward, bigStakedAmount)
	reward.Mul(reward, bigStakedDuration)
	reward.Div(reward, adjustedConsumptionRateDenominator)
	reward.Div(reward, bigCurrentSupply)
	reward.Div(reward, c.mintingPeriod)

	if !reward.IsUint64() {
		return remainingSupply
	}

	finalReward := reward.Uint64()
	return min(remainingSupply, finalReward)
}

type primaryNetworkCalculator struct {
	config        Config
	upgradeConfig upgrade.Config
}

// NewPrimaryNetworkCalculator returns a calculator for primary network staking
// rewards. It applies primary network reward upgrades.
func NewPrimaryNetworkCalculator(c Config, upgradeConfig upgrade.Config) Calculator {
	return &primaryNetworkCalculator{
		config:        c,
		upgradeConfig: upgradeConfig,
	}
}

func (c *primaryNetworkCalculator) Calculate(stakeStartTime time.Time, stakedDuration time.Duration, stakedAmount, currentSupply uint64) uint64 {
	cfg := configForStakeStart(c.config, c.upgradeConfig, stakeStartTime)
	return NewCalculator(cfg).Calculate(stakeStartTime, stakedDuration, stakedAmount, currentSupply)
}

func configForStakeStart(
	rewardConfig Config,
	upgradeConfig upgrade.Config,
	stakeStartTime time.Time,
) Config {
	const (
		// ACP-285 lowers primary network MinConsumptionRate to 7.5%.
		heliconMinConsumptionRateTarget          uint64 = 75_000
		heliconMinConsumptionRateReductionPeriod        = 90 * 24 * time.Hour
	)

	if !upgradeConfig.IsHeliconActivated(stakeStartTime) ||
		rewardConfig.MinConsumptionRate <= heliconMinConsumptionRateTarget {
		return rewardConfig
	}

	fullReduction := rewardConfig.MinConsumptionRate - heliconMinConsumptionRateTarget
	reduction := fullReduction
	if reductionEndTime := upgradeConfig.HeliconTime.Add(heliconMinConsumptionRateReductionPeriod); stakeStartTime.Before(reductionEndTime) {
		elapsed := stakeStartTime.Sub(upgradeConfig.HeliconTime)
		rampedReduction := new(big.Int).SetUint64(fullReduction)
		rampedReduction.Mul(rampedReduction, big.NewInt(int64(elapsed)))
		rampedReduction.Div(rampedReduction, big.NewInt(int64(heliconMinConsumptionRateReductionPeriod)))
		reduction = rampedReduction.Uint64()
	}

	rewardConfig.MinConsumptionRate -= reduction
	return rewardConfig
}

// Split [totalAmount] into [totalAmount * shares percentage] and the remainder.
//
// Invariant: [shares] <= [PercentDenominator]
func Split(totalAmount uint64, shares uint32) (uint64, uint64) {
	remainderShares := PercentDenominator - uint64(shares)
	remainderAmount := remainderShares * (totalAmount / PercentDenominator)

	// Delay rounding as long as possible for small numbers
	if optimisticReward, err := math.Mul(remainderShares, totalAmount); err == nil {
		remainderAmount = optimisticReward / PercentDenominator
	}

	amountFromShares := totalAmount - remainderAmount
	return amountFromShares, remainderAmount
}
