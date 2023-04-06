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
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/types"
)

const (
	interestRateBase        = 365 * 24 * 60 * 60
	interestRateDenominator = 1_000_000 * interestRateBase

	OfferFlagLocked uint64 = 0b1
)

var bigInterestRateDenominator = (&big.Int{}).SetInt64(interestRateDenominator)

type Offer struct {
	ID ids.ID `json:"id"`

	InterestRateNominator   uint64              `serialize:"true" json:"interestRateNominator"`   // deposit.Amount * (interestRateNominator / interestRateDenominator) == reward for deposit with 1 year duration
	Start                   uint64              `serialize:"true" json:"start"`                   // Unix time in seconds, when this offer becomes active (can be used to create new deposits)
	End                     uint64              `serialize:"true" json:"end"`                     // Unix time in seconds, when this offer becomes inactive (can't be used to create new deposits)
	MinAmount               uint64              `serialize:"true" json:"minAmount"`               // Minimum amount that can be deposited with this offer
	TotalMaxAmount          uint64              `serialize:"true" json:"totalMaxAmount"`          // Maximum amount that can be deposited with this offer in total (across all deposits)
	DepositedAmount         uint64              `serialize:"true" json:"depositedAmount"`         // Amount that was already deposited with this offer
	MinDuration             uint32              `serialize:"true" json:"minDuration"`             // Minimum duration of deposit created with this offer
	MaxDuration             uint32              `serialize:"true" json:"maxDuration"`             // Maximum duration of deposit created with this offer
	UnlockPeriodDuration    uint32              `serialize:"true" json:"unlockPeriodDuration"`    // Duration of period during which tokens deposited with this offer will be unlocked. The unlock period starts at the end of deposit minus unlockPeriodDuration
	NoRewardsPeriodDuration uint32              `serialize:"true" json:"noRewardsPeriodDuration"` // Duration of period during which rewards won't be accumulated. No rewards period starts at the end of deposit minus unlockPeriodDuration
	Memo                    types.JSONByteSlice `serialize:"true" json:"memo"`                    // Arbitrary offer memo
	Flags                   uint64              `serialize:"true" json:"flags"`                   // Bitfield with flags
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

func (o *Offer) RemainingAmount() uint64 {
	return o.TotalMaxAmount - o.DepositedAmount
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

	if len(o.Memo) > avax.MaxMemoSize {
		return fmt.Errorf("deposit offer memo is larger (%d bytes) than max of %d bytes", len(o.Memo), avax.MaxMemoSize)
	}

	return nil
}

type Deposit struct {
	DepositOfferID      ids.ID `serialize:"true"` // ID of deposit offer that was used to create this deposit
	UnlockedAmount      uint64 `serialize:"true"` // How many tokens have already been unlocked from this deposit
	ClaimedRewardAmount uint64 `serialize:"true"` // How many reward tokens have already been claimed for this deposit
	Start               uint64 `serialize:"true"` // Timestamp of time, when this deposit was created
	Duration            uint32 `serialize:"true"` // Duration of this deposit in seconds
	Amount              uint64 `serialize:"true"` // How many tokens were locked with this deposit
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
