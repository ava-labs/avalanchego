// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package configtest

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

// Shared Unit test setup utilities for a platform vm packages

var (
	MinStakingDuration = 24 * time.Hour
	MaxStakingDuration = 365 * 24 * time.Hour

	MinValidatorStake = 5 * units.MilliAvax
	MaxValidatorStake = 500 * units.MilliAvax

	Balance = 100 * MinValidatorStake
	Weight  = MinValidatorStake
)
