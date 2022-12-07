// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package deposit

import (
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

const (
	interestRateBase        = 365 * 24 * 60 * 60
	interestRateDenominator = 1_000_000 * interestRateBase
)

var bigInterestRateDenominator = (&big.Int{}).SetInt64(interestRateDenominator)

type Offer struct {
	ID ids.ID

	UnlockHalfPeriodDuration uint32 `serialize:"true"`
	InterestRateNominator    uint64 `serialize:"true"`
	Start                    uint64 `serialize:"true"`
	End                      uint64 `serialize:"true"`
	MinAmount                uint64 `serialize:"true"`
	MinDuration              uint32 `serialize:"true"`
	MaxDuration              uint32 `serialize:"true"`
}

// Sets offer id from its bytes hash
func (o *Offer) SetID() error {
	bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, o)
	if err != nil {
		return err
	}
	o.ID = hashing.ComputeHash256Array(bytes)
	return nil
}

// Time when this offer becomes active
func (o *Offer) StartTime() time.Time {
	return time.Unix(int64(o.Start), 0)
}

// Time when this offer becomes inactive
func (o *Offer) EndTime() time.Time {
	return time.Unix(int64(o.End), 0)
}

func (o *Offer) InterestRateFloat64() float64 {
	return float64(o.InterestRateNominator) / float64(interestRateDenominator)
}

type Deposit struct {
	DepositOfferID      ids.ID `serialize:"true"`
	UnlockedAmount      uint64 `serialize:"true"`
	ClaimedRewardAmount uint64 `serialize:"true"`
	Start               uint64 `serialize:"true"`
	Duration            uint32 `serialize:"true"`
	Amount              uint64 `serialize:"true"`
}

func (deposit *Deposit) StartTime() time.Time {
	return time.Unix(int64(deposit.Start), 0)
}

func (deposit *Deposit) IsExpired(
	depositOffer *Offer,
	timestamp uint64,
) bool {
	return deposit.Start+uint64(deposit.Duration)+uint64(depositOffer.UnlockHalfPeriodDuration) < timestamp
}

// Returns amount of tokens that can be unlocked from [deposit] at [unlockTime] (seconds).
//
// Precondition: all args are valid in conjunction.
func (deposit *Deposit) UnlockableAmount(offer *Offer, unlockTime uint64) uint64 {
	unlockPeriodStart, err := math.Add64(
		deposit.Start,
		uint64(deposit.Duration-offer.UnlockHalfPeriodDuration),
	)
	if err != nil || unlockPeriodStart > unlockTime {
		// if err (overflow), than unlockPeriodStart > unlockTime for sure
		return 0
	}

	unlockPeriodDuration := uint64(offer.UnlockHalfPeriodDuration) * 2
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
func (deposit *Deposit) ClaimableReward(offer *Offer, depositAmount, claimTime uint64) uint64 {
	if deposit.Start > claimTime {
		return 0
	}

	bigTotalRewardAmount := (&big.Int{}).SetUint64(depositAmount)
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
	bigDepositDuration := (&big.Int{}).SetUint64(uint64(deposit.Duration))
	bigInterestRateNominator := (&big.Int{}).SetUint64(offer.InterestRateNominator)

	// totalRewardAmount := depositAmount * offer.InterestRate * depositDuration / interestRateBase
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigDepositDuration)
	bigTotalRewardAmount.Mul(bigTotalRewardAmount, bigInterestRateNominator)
	bigTotalRewardAmount.Div(bigTotalRewardAmount, bigInterestRateDenominator)

	return bigTotalRewardAmount.Uint64()
}
