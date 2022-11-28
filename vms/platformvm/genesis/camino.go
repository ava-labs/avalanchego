// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Camino genesis args
type Camino struct {
	VerifyNodeSignature bool           `serialize:"true" json:"verifyNodeSignature"`
	LockModeBondDeposit bool           `serialize:"true" json:"lockModeBondDeposit"`
	InitialAdmin        ids.ShortID    `serialize:"true" json:"initialAdmin"`
	DepositOffers       []DepositOffer `serialize:"true" json:"depositOffers"`
}

type DepositOffer struct {
	UnlockHalfPeriodDuration uint32 `serialize:"true" json:"unlockHalfPeriodDuration"`
	InterestRateNominator    uint64 `serialize:"true" json:"interestRateNominator"`
	Start                    uint64 `serialize:"true" json:"start"`
	End                      uint64 `serialize:"true" json:"end"`
	MinAmount                uint64 `serialize:"true" json:"minAmount"`
	MinDuration              uint32 `serialize:"true" json:"minDuration"`
	MaxDuration              uint32 `serialize:"true" json:"maxDuration"`
}
