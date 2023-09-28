// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/math"
)

// Calculate returns the amount of tokens to reward a staker with.
//
// RemainingSupply = SupplyCap - ExistingSupply
// PortionOfExistingSupply = StakedAmount / ExistingSupply
// PortionOfStakingDuration = StakingDuration / MaximumStakingDuration
// MintingRate = MinMintingRate + MaxSubMinMintingRate * PortionOfStakingDuration
// Reward = RemainingSupply * PortionOfExistingSupply * MintingRate * PortionOfStakingDuration
func Calculate(
	config Config,
	stakedDuration time.Duration,
	stakedAmount uint64,
	currentSupply uint64,
) uint64 {
	bigMaxSubMinConsumptionRate := new(big.Int).SetUint64(config.MaxConsumptionRate - config.MinConsumptionRate)
	bigMinConsumptionRate := new(big.Int).SetUint64(config.MinConsumptionRate)
	bigMintingPeriod := new(big.Int).SetUint64(uint64(config.MintingPeriod))
	bigStakedDuration := new(big.Int).SetUint64(uint64(stakedDuration))
	bigStakedAmount := new(big.Int).SetUint64(stakedAmount)
	bigCurrentSupply := new(big.Int).SetUint64(currentSupply)

	adjustedConsumptionRateNumerator := new(big.Int).Mul(bigMaxSubMinConsumptionRate, bigStakedDuration)
	adjustedMinConsumptionRateNumerator := new(big.Int).Mul(bigMinConsumptionRate, bigMintingPeriod)
	adjustedConsumptionRateNumerator.Add(adjustedConsumptionRateNumerator, adjustedMinConsumptionRateNumerator)
	adjustedConsumptionRateDenominator := new(big.Int).Mul(bigMintingPeriod, consumptionRateDenominator)

	remainingSupply := config.SupplyCap - currentSupply
	reward := new(big.Int).SetUint64(remainingSupply)
	reward.Mul(reward, adjustedConsumptionRateNumerator)
	reward.Mul(reward, bigStakedAmount)
	reward.Mul(reward, bigStakedDuration)
	reward.Div(reward, adjustedConsumptionRateDenominator)
	reward.Div(reward, bigCurrentSupply)
	reward.Div(reward, bigMintingPeriod)

	if !reward.IsUint64() {
		return remainingSupply
	}

	finalReward := reward.Uint64()
	if finalReward > remainingSupply {
		return remainingSupply
	}

	return finalReward
}

// Split [totalAmount] into [totalAmount * shares percentage] and the remainder.
//
// Invariant: [shares] <= [PercentDenominator]
func Split(totalAmount uint64, shares uint32) (uint64, uint64) {
	remainderShares := PercentDenominator - uint64(shares)
	remainderAmount := remainderShares * (totalAmount / PercentDenominator)

	// Delay rounding as long as possible for small numbers
	if optimisticReward, err := math.Mul64(remainderShares, totalAmount); err == nil {
		remainderAmount = optimisticReward / PercentDenominator
	}

	amountFromShares := totalAmount - remainderAmount
	return amountFromShares, remainderAmount
}
