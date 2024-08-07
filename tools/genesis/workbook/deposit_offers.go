// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
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

func (*DepositOfferRow) Header() []string {
	return []string{"OfferID", "Title", "interestRateNominator", "startOffset"}
}

func (offer *DepositOfferRow) FromRow(_ int, row []string) error {
	// COLUMNS
	const (
		offerID DepositOfferColumn = iota
		title
		interestRateNominator
		startOffset
		endOffset
		minAmount
		minDuration
		maxDuration
		unlockPeriodDuration
		noRewardsPeriodDuration
		locked
		_comment
	)
	var err error

	offer.OfferID = row[offerID]
	offer.Memo = row[title]

	offer.InterestRateNominator, err = strconv.ParseUint(row[interestRateNominator], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse interest rate nominator %s, err %w", row[interestRateNominator], err)
	}

	offer.StartOffset, err = strconv.ParseUint(row[startOffset], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse start offset %s, err %w", row[startOffset], err)
	}

	offer.EndOffset, err = strconv.ParseUint(row[endOffset], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse end offset %s, err %w", row[endOffset], err)
	}

	offer.MinAmount, err = strconv.ParseUint(row[minAmount], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse min amount %s, err %w", row[minAmount], err)
	}

	minDurationUint64, err := strconv.ParseUint(row[minDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse min duration %s, err %w", row[minDuration], err)
	}
	offer.MinDuration = uint32(minDurationUint64)

	maxDurationUint64, err := strconv.ParseUint(row[maxDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse max duration %s, err %w", row[maxDuration], err)
	}
	offer.MaxDuration = uint32(maxDurationUint64)

	upd, err := strconv.ParseUint(row[unlockPeriodDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse unlock period duration %s, err %w", row[unlockPeriodDuration], err)
	}
	offer.UnlockPeriodDuration = uint32(upd)

	nrpd, err := strconv.ParseUint(row[noRewardsPeriodDuration], 10, 32)
	if err != nil {
		return fmt.Errorf("could not parse no rewards period duration %s, err %w", row[noRewardsPeriodDuration], err)
	}
	offer.NoRewardsPeriodDuration = uint32(nrpd)

	if row[locked] != TrueValue && row[locked] != FalseValue {
		return fmt.Errorf("locked value must be either TRUE or FALSE, got %s", row[locked])
	}
	offer.Locked = row[locked] == TrueValue

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
