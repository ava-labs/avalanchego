// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package workbook

import (
	"fmt"
	"strconv"

	"github.com/ava-labs/avalanchego/genesis"
)

type DepositOfferColumn int

type DepositOfferRow struct {
	OfferID                 string
	InterestRateNominator   uint64
	StartOffset             uint64
	EndOffset               uint64
	MinAmount               uint64
	MinDuration             uint32
	MaxDuration             uint32
	UnlockPeriodDuration    uint32
	NoRewardsPeriodDuration uint32
	Memo                    string
	Locked                  bool
}

func (offer *DepositOfferRow) Header() []string {
	return []string{"OfferID", "Title", "interestRateNominator", "startOffset"}
}

func (offer *DepositOfferRow) FromRow(_ int, row []string) error {
	// COLUMNS
	const (
		OfferID DepositOfferColumn = iota
		Title
		InterestRateNominator
		StartOffset
		EndOffset
		MinAmount
		MinDuration
		MaxDuration
		UnlockPeriodDuration
		NoRewardsPeriodDuration
		Locked
		_Comment
	)
	var err error

	offer.OfferID = row[OfferID]
	offer.Memo = row[Title]

	offer.InterestRateNominator, err = strconv.ParseUint(row[InterestRateNominator], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse interest rate nominator %s, err %w", row[InterestRateNominator], err)
	}

	offer.StartOffset, err = strconv.ParseUint(row[StartOffset], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse start offset %s, err %w", row[StartOffset], err)
	}

	offer.EndOffset, err = strconv.ParseUint(row[EndOffset], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse end offset %s, err %w", row[EndOffset], err)
	}

	offer.MinAmount, err = strconv.ParseUint(row[MinAmount], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse min amount %s, err %w", row[MinAmount], err)
	}

	minDuration, err := strconv.ParseUint(row[MinDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse min duration %s, err %w", row[MinDuration], err)
	}
	offer.MinDuration = uint32(minDuration)

	maxDuration, err := strconv.ParseUint(row[MaxDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse max duration %s, err %w", row[MaxDuration], err)
	}
	offer.MaxDuration = uint32(maxDuration)

	upd, err := strconv.ParseUint(row[UnlockPeriodDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse unlock period duration %s, err %w", row[UnlockPeriodDuration], err)
	}
	offer.UnlockPeriodDuration = uint32(upd)

	nrpd, err := strconv.ParseUint(row[NoRewardsPeriodDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse no rewards period duration %s, err %w", row[NoRewardsPeriodDuration], err)
	}
	offer.NoRewardsPeriodDuration = uint32(nrpd)

	if row[Locked] != TrueValue && row[Locked] != FalseValue {
		return fmt.Errorf("locked value must be either TRUE or FALSE, got %s", row[Locked])
	}
	offer.Locked = row[Locked] == TrueValue

	return nil
}

func RowToOffer(offer *DepositOfferRow) (string, *genesis.UnparsedDepositOffer) {
	udo := &genesis.UnparsedDepositOffer{
		InterestRateNominator:   offer.InterestRateNominator,
		StartOffset:             offer.StartOffset,
		EndOffset:               offer.EndOffset,
		MinAmount:               offer.MinAmount,
		MinDuration:             offer.MinDuration,
		MaxDuration:             offer.MaxDuration,
		UnlockPeriodDuration:    offer.UnlockPeriodDuration,
		NoRewardsPeriodDuration: offer.NoRewardsPeriodDuration,
		Memo:                    offer.Memo,
		Flags:                   genesis.UnparsedDepositOfferFlags{Locked: offer.Locked},
	}

	return offer.OfferID, udo
}
