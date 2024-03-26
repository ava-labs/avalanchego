// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"
)

// Once E upgrade is activate users can specify an optional tip on top of
// the required fee, to incentivize inclusion of their transactions.
// Tip is expressed as a percentage of the base fee and it is burned as the required fee.

const (
	NoTip            = 0
	MaxTipPercentage = 1_000
)

var (
	errTipPercentageNegative = errors.New("tip percentage negative")
	errTipPercentageTooLarge = errors.New("tip percentage too large")
)

type TipPercentage int

func (t TipPercentage) Validate() error {
	if t < 0 {
		return fmt.Errorf("%w, tip percentage %d", errTipPercentageNegative, t)
	}

	if t > MaxTipPercentage {
		return fmt.Errorf("%w, tip percentage %d", errTipPercentageTooLarge, t)
	}

	return nil
}
