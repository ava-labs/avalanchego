// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package deposit

import (
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

type Deposit struct {
	DepositOfferID      ids.ID   `serialize:"true"` // ID of deposit offer that was used to create this deposit
	UnlockedAmount      uint64   `serialize:"true"` // How many tokens have already been unlocked from this deposit
	ClaimedRewardAmount uint64   `serialize:"true"` // How many reward tokens have already been claimed for this deposit
	Start               uint64   `serialize:"true"` // Timestamp of time, when this deposit was created
	Duration            uint32   `serialize:"true"` // Duration of this deposit in seconds
	Amount              uint64   `serialize:"true"` // How many tokens were locked with this deposit
	RewardOwner         fx.Owner `serialize:"true"` // The owner who has right to claim rewards for this deposit
}

func (deposit *Deposit) StartTime() time.Time {
	return time.Unix(int64(deposit.Start), 0)
}

func (deposit *Deposit) EndTime() time.Time {
	return deposit.StartTime().Add(time.Duration(deposit.Duration) * time.Second)
}

func (deposit *Deposit) IsExpired(timestamp uint64) bool {
	depositEndTimestamp, err := math.Add64(deposit.Start, uint64(deposit.Duration))
	if err != nil {
		// if err (overflow), than depositEndTimestamp > timestamp
		return false
	}
	return depositEndTimestamp <= timestamp
}

// Returns amount of tokens that can be unlocked from [deposit] at [unlockTime] (seconds).
//
// Precondition: all args are valid in conjunction.
func (deposit *Deposit) UnlockableAmount(offer *Offer, unlockTime uint64) uint64 {
	unlockPeriodStart, err := math.Add64(
		deposit.Start,
		uint64(deposit.Duration-offer.UnlockPeriodDuration),
	)
	if err != nil || unlockPeriodStart > unlockTime {
		// if err (overflow), than unlockPeriodStart > unlockTime for sure
		return 0
	}

	if offer.UnlockPeriodDuration == 0 {
		return deposit.Amount - deposit.UnlockedAmount
	}

	unlockPeriodDuration := uint64(offer.UnlockPeriodDuration)
	passedUnlockPeriodDuration := math.Min(unlockTime-unlockPeriodStart, unlockPeriodDuration)

	bigTotalUnlockableAmount := (&big.Int{}).SetUint64(deposit.Amount)
	bigPassedUnlockPeriodDuration := (&big.Int{}).SetUint64(passedUnlockPeriodDuration)
	bigUnlockPeriodDuration := (&big.Int{}).SetUint64(unlockPeriodDuration)

	// totalUnlockableAmount := depositAmount * passedUnlockPeriodDuration / unlockPeriodDuration
	bigTotalUnlockableAmount.Mul(bigTotalUnlockableAmount, bigPassedUnlockPeriodDuration)
	bigTotalUnlockableAmount.Div(bigTotalUnlockableAmount, bigUnlockPeriodDuration)

	return bigTotalUnlockableAmount.Uint64() - deposit.UnlockedAmount
}

// Returns amount of tokens that can be claimed as reward for [deposit] at [claimetime] (seconds).
//
// Precondition: all args are valid in conjunction.
func (deposit *Deposit) ClaimableReward(offer *Offer, claimTime uint64) uint64 {
	if deposit.Start > claimTime {
		return 0
	}

	rewardsEndTime, err := math.Add64(
		deposit.Start,
		uint64(deposit.Duration-offer.NoRewardsPeriodDuration),
	)
	if err != nil {
		// if err (overflow), than rewardsEndTime > claimTime
		rewardsEndTime = claimTime
	}

	claimTime = math.Min(claimTime, rewardsEndTime)

	bigTotalRewardAmount := (&big.Int{}).SetUint64(deposit.Amount)
	bigPassedDepositDuration := (&big.Int{}).SetUint64(claimTime - deposit.Start)
	bigInterestRateNominator := (&big.Int{}).SetUint64(offer.InterestRateNominator)

	// totalRewardAmount := depositAmount * offer.InterestRate * passedDepositDuration / interestRateBase
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigPassedDepositDuration)
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigInterestRateNominator)
	bigTotalRewardAmount.Div(bigTotalRewardAmount, bigInterestRateDenominator)

	return bigTotalRewardAmount.Uint64() - deposit.ClaimedRewardAmount
}

// Returns amount of tokens that can be claimed as reward for [depositAmount].
//
// Precondition: all args are valid in conjunction.
func (deposit *Deposit) TotalReward(offer *Offer) uint64 {
	bigTotalRewardAmount := (&big.Int{}).SetUint64(deposit.Amount)

	// rewardsPeriodDuration = deposit.Duration - offer.NoRewardsPeriodDuration
	bigRewardsPeriodDuration := (&big.Int{}).SetUint64(uint64(deposit.Duration - offer.NoRewardsPeriodDuration))
	bigInterestRateNominator := (&big.Int{}).SetUint64(offer.InterestRateNominator)

	// totalRewardAmount := depositAmount * offer.InterestRate * rewardsPeriodDuration / interestRateBase
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigRewardsPeriodDuration)
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigInterestRateNominator)
	bigTotalRewardAmount.Div(bigTotalRewardAmount, bigInterestRateDenominator)

	return bigTotalRewardAmount.Uint64()
}
