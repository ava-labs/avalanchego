// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func Clock(forkTime time.Time) *mockable.Clock {
	clk := &mockable.Clock{}
	clk.Set(forkTime)
	return clk
}
