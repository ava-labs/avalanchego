// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package deposit

import (
	"errors"
	"fmt"
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

	OfferFlagLocked uint64 = 0b1
)

var bigInterestRateDenominator = (&big.Int{}).SetInt64(interestRateDenominator)

type Offer struct {
	ID ids.ID

	InterestRateNominator   uint64 `serialize:"true" json:"InterestRateNominator"`
	Start                   uint64 `serialize:"true" json:"Start"`
	End                     uint64 `serialize:"true" json:"End"`
	MinAmount               uint64 `serialize:"true" json:"MinAmount"`
	MinDuration             uint32 `serialize:"true" json:"MinDuration"`
	MaxDuration             uint32 `serialize:"true" json:"MaxDuration"`
	UnlockPeriodDuration    uint32 `serialize:"true" json:"UnlockPeriodDuration"`
	NoRewardsPeriodDuration uint32 `serialize:"true" json:"NoRewardsPeriodDuration"`
	Memo                    []byte `serialize:"true" json:"Memo"`
	Flags                   uint64 `serialize:"true" json:"Flags"`
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

func (o *Offer) Verify() error {
	if o.Start >= o.End {
		return fmt.Errorf(
			"deposit offer starttime (%v) is not before its endtime (%v)",
			o.Start,
			o.End,
		)
	}

	if o.MinDuration > o.MaxDuration {
		return errors.New("deposit minimum duration is greater than maximum duration")
	}

	if o.MinDuration == 0 {
		return errors.New("deposit offer has zero minimum duration")
	}

	if o.MinDuration < o.NoRewardsPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than no-rewards period duration (%v)",
			o.MinDuration,
			o.NoRewardsPeriodDuration,
		)
	}

	if o.MinDuration < o.UnlockPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than unlock period duration (%v)",
			o.MinDuration,
			o.UnlockPeriodDuration,
		)
	}

	return nil
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
