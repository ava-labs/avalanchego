// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package deposit

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/types"
)

const (
	interestRateBase               = 365 * 24 * 60 * 60
	interestRateDenominator        = 1_000_000 * interestRateBase
	OfferMinDepositAmount   uint64 = 1 * units.MilliAvax
)

var (
	bigInterestRateDenominator = (&big.Int{}).SetInt64(interestRateDenominator)

	errWrongLimitValues           = errors.New("can only use either TotalMaxAmount or TotalMaxRewardAmount")
	errDepositedMoreThanMaxAmount = errors.New("offer deposited amount is more than offer total max amount")
	errRewardedMoreThanMaxAmount  = errors.New("offer deposits total reward amount is more than offer total max reward amount")
	errMinAmountTooSmall          = errors.New("offer minAmount is too small")
	errMinAmountTooBig            = errors.New("offer minAmount is too big")
	errWrongRewardValues          = errors.New("offer interest rate and total max reward amount must both be zero or not zero")
)

type OfferFlag uint64

const (
	OfferFlagNone   OfferFlag = 0
	OfferFlagLocked OfferFlag = 0b1
)

type Offer struct {
	UpgradeVersionID codec.UpgradeVersionID
	ID               ids.ID

	InterestRateNominator   uint64              `serialize:"true" json:"interestRateNominator"`                   // deposit.Amount * (interestRateNominator / interestRateDenominator) == reward for deposit with 1 year duration
	Start                   uint64              `serialize:"true" json:"start"`                                   // Unix time in seconds, when this offer becomes active (can be used to create new deposits)
	End                     uint64              `serialize:"true" json:"end"`                                     // Unix time in seconds, when this offer becomes inactive (can't be used to create new deposits)
	MinAmount               uint64              `serialize:"true" json:"minAmount"`                               // Minimum amount that can be deposited with this offer
	TotalMaxAmount          uint64              `serialize:"true" json:"totalMaxAmount"`                          // Maximum amount that can be deposited with this offer in total (across all deposits)
	DepositedAmount         uint64              `serialize:"true" json:"depositedAmount"`                         // Amount that was already deposited with this offer
	MinDuration             uint32              `serialize:"true" json:"minDuration"`                             // Minimum duration of deposit created with this offer
	MaxDuration             uint32              `serialize:"true" json:"maxDuration"`                             // Maximum duration of deposit created with this offer
	UnlockPeriodDuration    uint32              `serialize:"true" json:"unlockPeriodDuration"`                    // Duration of period during which tokens deposited with this offer will be unlocked. The unlock period starts at the end of deposit minus unlockPeriodDuration
	NoRewardsPeriodDuration uint32              `serialize:"true" json:"noRewardsPeriodDuration"`                 // Duration of period during which rewards won't be accumulated. No rewards period starts at the end of deposit minus unlockPeriodDuration
	Memo                    types.JSONByteSlice `serialize:"true" json:"memo"`                                    // Arbitrary offer memo
	Flags                   OfferFlag           `serialize:"true" json:"flags"`                                   // Bitfield with flags
	TotalMaxRewardAmount    uint64              `serialize:"true" json:"totalMaxRewardAmount" upgradeVersion:"1"` // Maximum amount that can be rewarded for all deposits created with this offer in total
	RewardedAmount          uint64              `serialize:"true" json:"rewardedAmount"       upgradeVersion:"1"` // Amount that was already rewarded (including potential rewards) for deposits created with this offer
	OwnerAddress            ids.ShortID         `serialize:"true" json:"ownerAddress"         upgradeVersion:"1"` // Address that can sign deposit-creator permission
}

// Time when this offer becomes active
func (o *Offer) StartTime() time.Time {
	return time.Unix(int64(o.Start), 0)
}

// Time when this offer becomes inactive
func (o *Offer) EndTime() time.Time {
	return time.Unix(int64(o.End), 0)
}

// Return 0 if o.TotalMaxAmount is 0.
func (o *Offer) RemainingAmount() uint64 {
	return o.TotalMaxAmount - o.DepositedAmount
}

// Returns maximum possible amount that could be deposited with this offer. Return 0 if o.TotalMaxRewardAmount is 0.
func (o *Offer) MaxRemainingAmountByReward() uint64 {
	// maxRewardsPeriodDuration = offer.MaxDuration - offer.NoRewardsPeriodDuration
	// using MaxDuration, cause its the case, where most reward per deposit amount is issued
	maxRewardsPeriodDuration := (&big.Int{}).SetUint64(uint64(o.MaxDuration - o.NoRewardsPeriodDuration))

	// maxDepositAmount = remainingRewardAmount * interestRateBase / (offer.InterestRate * rewardsPeriodDuration)
	denominator := (&big.Int{}).SetUint64(o.InterestRateNominator)
	denominator.Mul(denominator, maxRewardsPeriodDuration)

	nominator := (&big.Int{}).SetUint64(o.RemainingReward())
	nominator.Mul(nominator, bigInterestRateDenominator)

	return nominator.Div(nominator, denominator).Uint64()
}

// Return 0 if o.TotalMaxRewardAmount is 0.
func (o *Offer) RemainingReward() uint64 {
	return o.TotalMaxRewardAmount - o.RewardedAmount
}

// Calculates max remaining reward amount by using offer max duration. Return 0 if o.TotalMaxAmount is 0.
func (o *Offer) MaxRemainingRewardByTotalMaxAmount() uint64 {
	dummyDeposit := &Deposit{
		Amount:   o.RemainingAmount(),
		Duration: o.MaxDuration,
	}
	return dummyDeposit.TotalReward(o)
}

func (o *Offer) IsActiveAt(timestamp uint64) bool {
	return o.Start <= timestamp && timestamp <= o.End && o.Flags&OfferFlagLocked == 0
}

func (o *Offer) InterestRateFloat64() float64 {
	return float64(o.InterestRateNominator) / float64(interestRateDenominator)
}

func (o *Offer) Verify() error {
	// common checks
	switch {
	case o.Start >= o.End:
		return fmt.Errorf(
			"deposit offer starttime (%v) is not before its endtime (%v)",
			o.Start,
			o.End,
		)
	case o.MinDuration > o.MaxDuration:
		return errors.New("deposit minimum duration is greater than maximum duration")
	case o.MinDuration == 0:
		return errors.New("deposit offer has zero minimum duration")
	case o.MinDuration < o.NoRewardsPeriodDuration:
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than no-rewards period duration (%v)",
			o.MinDuration,
			o.NoRewardsPeriodDuration,
		)
	case o.MinDuration < o.UnlockPeriodDuration:
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than unlock period duration (%v)",
			o.MinDuration,
			o.UnlockPeriodDuration,
		)
	case len(o.Memo) > avax.MaxMemoSize:
		return fmt.Errorf("deposit offer memo is larger (%d bytes) than max of %d bytes", len(o.Memo), avax.MaxMemoSize)
	case o.DepositedAmount > o.TotalMaxAmount:
		return errDepositedMoreThanMaxAmount
	case o.RewardedAmount > o.TotalMaxRewardAmount:
		return errRewardedMoreThanMaxAmount
	}

	// version-specific checks
	if o.UpgradeVersionID.Version() > 0 {
		if o.TotalMaxRewardAmount != 0 {
			// maxRewardsPeriodDuration = offer.MaxDuration - offer.NoRewardsPeriodDuration
			// using MaxDuration, cause its the case, where most reward per deposit amount is issued
			maxRewardsPeriodDuration := (&big.Int{}).SetUint64(uint64(o.MaxDuration - o.NoRewardsPeriodDuration))

			// maxDepositAmount = offer.TotalMaxRewardAmount * interestRateBase / (offer.InterestRate * rewardsPeriodDuration)
			denominator := (&big.Int{}).SetUint64(o.InterestRateNominator)
			denominator.Mul(denominator, maxRewardsPeriodDuration)

			nominator := (&big.Int{}).SetUint64(o.TotalMaxRewardAmount)
			nominator.Mul(nominator, bigInterestRateDenominator)

			maxDepositAmount := nominator.Div(nominator, denominator).Uint64()
			if o.MinAmount > maxDepositAmount {
				return errMinAmountTooBig
			}
		}

		switch {
		case o.TotalMaxAmount != 0 && o.TotalMaxRewardAmount != 0:
			return errWrongLimitValues
		case o.MinAmount < OfferMinDepositAmount:
			return errMinAmountTooSmall
		case o.TotalMaxRewardAmount == 0 && o.InterestRateNominator != 0 ||
			o.TotalMaxRewardAmount != 0 && o.InterestRateNominator == 0:
			return errWrongRewardValues
		}
	}

	return nil
}

func (o *Offer) PermissionMsg(depositCreatorAddres ids.ShortID) []byte {
	msg := make([]byte, len(o.ID)+len(depositCreatorAddres))
	copy(msg, o.ID[:])
	copy(msg[len(o.ID):], depositCreatorAddres[:])
	return msg
}
