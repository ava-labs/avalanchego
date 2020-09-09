// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math/big"
	"time"
)

var (
	maxSubMinConsumptionRate   = new(big.Int).SetUint64(MaxSubMinConsumptionRate)
	minConsumptionRate         = new(big.Int).SetUint64(MinConsumptionRate)
	consumptionRateDenominator = new(big.Int).SetUint64(PercentDenominator)
	consumptionInterval        = new(big.Int).SetUint64(uint64(MaximumStakingDuration))
)

type rewardTx struct {
	Reward uint64 `serialize:"true"`
	Tx     Tx     `serialize:"true"`
}

// Reward returns the amount of tokens to reward the staker with.
//
// RemainingSupply = SupplyCap - ExistingSupply
// PortionOfExistingSupply = StakedAmount / ExistingSupply
// PortionOfStakingDuration = StakingDuration / MaximumStakingDuration
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
	reward.Mul(reward, stakedAmount)
	reward.Mul(reward, duration)
	reward.Div(reward, adjustedConsumptionRateDenominator)
	reward.Div(reward, maxExistingAmount)
	reward.Div(reward, consumptionInterval)

	return reward.Uint64()
}
