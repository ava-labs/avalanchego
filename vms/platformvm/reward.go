// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"math/big"
	"time"
)

var (
	// InflationRate ...
	InflationRate = 1.0
)

// reward returns the amount of tokens to reward the staker with
func reward(duration time.Duration, amount uint64, inflationRate float64) uint64 {
	// TODO: Can't use floats here. Need to figure out how to do some integer
	// approximations

	years := duration.Hours() / (365. * 24.)

	// Total value of this transaction
	value := float64(amount) * math.Pow(inflationRate, years)

	// Amount of the reward
	reward := value - float64(amount)

	return uint64(reward)
}

var (
	maxSubMinConsumptionRate   = new(big.Int).SetUint64(MaxSubMinConsumptionRate)
	minConsumptionRate         = new(big.Int).SetUint64(MinConsumptionRate)
	consumptionRateDenominator = new(big.Int).SetUint64(PercentDenominator)
	consumptionInterval        = new(big.Int).SetUint64(uint64(MaximumStakingDuration))
)

// Reward returns the amount of tokens to reward the staker with.
//
// RemainingSupply = SupplyCap - ExistingSupply
// PortionOfExistingSupply = StakedAmount / ExistingSupply
// PortionOfStakingDuration = MinimumStakingDuration / MaximumStakingDuration
// MintingRate = MinMintingRate + MaxSubMinMintingRate * PortionOfStakingDuration
// Reward = RemainingSupply * PortionOfExistingSupply * MintingRate * PortionOfStakingDuration
func Reward(
	rawDuration time.Duration,
	rawStakedAmount,
	rawMaxExistingAmount uint64,
) uint64 {
	duration := new(big.Int).SetUint64(uint64(rawDuration))
	stakedAmount := new(big.Int).SetUint64(rawStakedAmount)
	maxExistingAmount := new(big.Int).SetUint64(rawMaxExistingAmount)

	adjustedConsumptionRateNumerator := new(big.Int).Mul(maxSubMinConsumptionRate, duration)
	adjustedMinConsumptionRateNumerator := new(big.Int).Mul(minConsumptionRate, consumptionInterval)
	adjustedConsumptionRateNumerator.Add(adjustedConsumptionRateNumerator, adjustedMinConsumptionRateNumerator)
	adjustedConsumptionRateDenominator := new(big.Int).Mul(consumptionInterval, consumptionRateDenominator)

	reward := new(big.Int).SetUint64(SupplyCap - rawMaxExistingAmount)
	reward.Mul(reward, adjustedConsumptionRateNumerator)
	reward.Div(reward, adjustedConsumptionRateDenominator)
	reward.Mul(reward, stakedAmount)
	reward.Div(reward, maxExistingAmount)
	reward.Mul(reward, duration)
	reward.Div(reward, consumptionInterval)

	return reward.Uint64()
}
