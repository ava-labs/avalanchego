// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Camino genesis args
type Camino struct {
	VerifyNodeSignature bool           `serialize:"true" json:"verifyNodeSignature"`
	LockModeBondDeposit bool           `serialize:"true" json:"lockModeBondDeposit"`
	InitialAdmin        ids.ShortID    `serialize:"true" json:"initialAdmin"`
	DepositOffers       []DepositOffer `serialize:"true" json:"depositOffers"`
	Deposits            []*txs.Tx      `serialize:"true" json:"deposits"`
}

type DepositOffer struct {
	InterestRateNominator   uint64 `serialize:"true" json:"interestRateNominator"`
	Start                   uint64 `serialize:"true" json:"start"`
	End                     uint64 `serialize:"true" json:"end"`
	MinAmount               uint64 `serialize:"true" json:"minAmount"`
	MinDuration             uint32 `serialize:"true" json:"minDuration"`
	MaxDuration             uint32 `serialize:"true" json:"maxDuration"`
	UnlockPeriodDuration    uint32 `serialize:"true" json:"unlockPeriodDuration"`
	NoRewardsPeriodDuration uint32 `serialize:"true" json:"noRewardsPeriodDuration"`
	Flags                   uint64 `serialize:"true" json:"flags"`
}

// Gets offer id from its bytes hash
func (offer DepositOffer) ID() (ids.ID, error) {
	bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, offer)
	if err != nil {
		return ids.Empty, err
	}
	return hashing.ComputeHash256Array(bytes), nil
}

func (offer DepositOffer) Verify() error {
	if offer.Start >= offer.End {
		return fmt.Errorf(
			"deposit offer starttime (%v) is not before its endtime (%v)",
			offer.Start,
			offer.End,
		)
	}

	if offer.MinDuration > offer.MaxDuration {
		return errors.New("deposit minimum duration is greater than maximum duration")
	}

	if offer.MinDuration == 0 {
		return errors.New("deposit offer has zero minimum duration")
	}

	if offer.MinDuration < offer.NoRewardsPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than no-rewards period duration (%v)",
			offer.MinDuration,
			offer.NoRewardsPeriodDuration,
		)
	}

	if offer.MinDuration < offer.UnlockPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than unlock period duration (%v)",
			offer.MinDuration,
			offer.UnlockPeriodDuration,
		)
	}

	return nil
}
